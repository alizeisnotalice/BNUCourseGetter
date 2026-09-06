package portal

import (
	"context"
	"strings"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"github.com/mxschmitt/playwright-go"
)

func (s *Session) findFrame(ctx context.Context, controls ...string) (playwright.Frame, error) {
	var found playwright.Frame
	err := s.poll(ctx, func() (bool, error) {
		frames, err := descendantFrames(s.page.MainFrame())
		if err != nil {
			return false, operationError(ctx, err)
		}
		var matches []playwright.Frame
		for _, f := range frames {
			all := true
			for _, sel := range controls {
				if !visible(f.Locator(sel)) {
					all = false
					break
				}
			}
			if all {
				matches = append(matches, f)
			}
		}
		if len(matches) > 1 {
			return false, selection.Error("mismatch", "出现多个相同功能的框架，已停止以避免选错班级")
		}
		if len(matches) == 1 {
			found = matches[0]
			return true, nil
		}
		return false, nil
	})
	return found, err
}

// descendantFrames discovers frame hierarchy through iframe element handles.
// playwright-go v0.6100.0 mutates Page.Frames/Frame.ChildFrames from its event
// goroutine without synchronization, so reading those slices can race while a
// delayed frame attaches. DOM-backed ContentFrame calls avoid that library race.
func descendantFrames(parent playwright.Frame) ([]playwright.Frame, error) {
	result := []playwright.Frame{parent}
	handles, err := parent.Locator("iframe,frame").ElementHandles()
	if err != nil {
		return nil, err
	}
	for _, handle := range handles {
		child, err := handle.ContentFrame()
		if err != nil {
			return nil, err
		}
		if child == nil {
			continue
		}
		nested, err := descendantFrames(child)
		if err != nil {
			return nil, err
		}
		result = append(result, nested...)
	}
	return result, nil
}

func directFrames(parent playwright.Frame) ([]playwright.Frame, error) {
	handles, err := parent.Locator("iframe,frame").ElementHandles()
	if err != nil {
		return nil, err
	}
	result := make([]playwright.Frame, 0, len(handles))
	for _, handle := range handles {
		child, err := handle.ContentFrame()
		if err != nil {
			return nil, err
		}
		if child != nil {
			result = append(result, child)
		}
	}
	return result, nil
}

// Query forms in the supported legacy portal navigate their own frmReport
// child. Scope the wait to that named child instead of relying on frame order or
// catching an unrelated page resource.
func (s *Session) queryReport(ctx context.Context, parent playwright.Frame, action func() error) (playwright.Frame, error) {
	var report playwright.Frame
	if err := s.poll(ctx, func() (bool, error) {
		children, err := directFrames(parent)
		if err != nil {
			return false, operationError(ctx, err)
		}
		matches := []playwright.Frame{}
		for _, child := range children {
			if child.Name() == "frmReport" {
				matches = append(matches, child)
			}
		}
		if len(matches) > 1 {
			return false, selection.Error("mismatch", "查询结果框架不唯一")
		}
		if len(matches) == 1 {
			report = matches[0]
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	response, err := report.ExpectNavigation(action, playwright.FrameExpectNavigationOptions{
		Timeout:   playwright.Float(float64(s.opts.Timeout.Milliseconds())),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		if fault := s.fault(); fault != nil {
			return nil, fault
		}
		return nil, operationError(ctx, err)
	}
	if response.Status() >= 500 {
		return nil, selection.Error("transient", "教务查询服务暂时不可用")
	}
	if response.Status() != 200 {
		return nil, selection.Error("mismatch", "查询页面返回异常状态")
	}
	return report, nil
}

func (s *Session) openCategory(ctx context.Context) (playwright.Frame, error) {
	menu, title := "#JW130415", "#title1803"
	if s.target.Type == "major" {
		menu, title = "#JW130403", "#title1785"
	}
	if visible(s.page.Locator(menu)) {
		if err := s.page.Locator(menu).Click(); err != nil {
			return nil, operationError(ctx, err)
		}
	} else {
		if err := s.page.Locator("li[data-code='JW1304']").Click(); err != nil {
			return nil, operationError(ctx, err)
		}
		frame, err := s.findFrame(ctx, title)
		if err != nil {
			return nil, err
		}
		if err = frame.Locator(title).Click(); err != nil {
			return nil, operationError(ctx, err)
		}
	}
	return s.findFrame(ctx, "#kcmc", "#btnQry")
}

func (s *Session) Query(ctx context.Context) (selection.Candidate, error) {
	s.clearFault()
	s.classFrame = nil
	s.detailFrame = nil
	if err := s.login(ctx); err != nil {
		return selection.Candidate{}, err
	}
	frame, err := s.openCategory(ctx)
	if err != nil {
		return selection.Candidate{}, err
	}
	if disabled, err := frame.Locator("#kcmc").IsDisabled(); err != nil {
		return selection.Candidate{}, operationError(ctx, err)
	} else if disabled {
		return selection.Candidate{}, selection.Error("not_open", "当前不在有效选课时间区段")
	}
	s.term = readTerm(frame)
	if s.target.Type == "major" {
		all := frame.Locator("#kkdw_range_all")
		if !visible(all) {
			return selection.Candidate{}, selection.Error("mismatch", "未找到按开课计划查询控件")
		}
		if disabled, _ := all.IsDisabled(); disabled {
			return selection.Candidate{}, selection.Error("not_open", "当前不在有效选课时间区段")
		}
		if err = all.Click(); err != nil {
			return selection.Candidate{}, operationError(ctx, err)
		}
	}
	if err = frame.Locator("#kcmc").Fill(s.target.CourseID); err != nil {
		return selection.Candidate{}, operationError(ctx, err)
	}
	if s.target.Type == "public" {
		if err = frame.Locator("#t_skbh").Fill(s.target.ClassID); err != nil {
			return selection.Candidate{}, operationError(ctx, err)
		}
	}
	report, err := s.queryReport(ctx, frame, func() error { return frame.Locator("#btnQry").Click() })
	if err != nil {
		return selection.Candidate{}, err
	}
	if s.target.Type == "major" {
		var courseRow tableRow
		err = s.poll(ctx, func() (bool, error) {
			rows, err := readRows(report)
			if err != nil {
				return false, operationError(ctx, err)
			}
			matches := []tableRow{}
			for _, r := range rows {
				if r.hasCourse(s.target.CourseID) {
					matches = append(matches, r)
				}
			}
			if len(matches) > 1 {
				return false, selection.Error("mismatch", "检索到多个同代码课程，不能唯一定位")
			}
			if len(matches) == 1 {
				courseRow = matches[0]
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return selection.Candidate{}, err
		}
		// This operation opens the class chooser; it does not enroll the student.
		choose := report.Locator("tr").Nth(courseRow.Index).Locator("[id$='_operation'] a")
		if n, _ := choose.Count(); n != 1 {
			return selection.Candidate{}, selection.Error("mismatch", "未找到唯一的班级详情入口")
		}
		if err = choose.Click(); err != nil {
			return selection.Candidate{}, operationError(ctx, err)
		}
		detail, err := s.findFrame(ctx, "#btnSubmit", "#frmReport")
		if err != nil {
			return selection.Candidate{}, err
		}
		s.detailFrame = detail
		// The report is scoped to its verified class chooser parent.
		err = s.poll(ctx, func() (bool, error) {
			children, err := directFrames(detail)
			if err != nil {
				return false, operationError(ctx, err)
			}
			var found []playwright.Frame
			for _, f := range children {
				if f.Name() == "frmReport" {
					found = append(found, f)
				}
			}
			if len(found) > 1 {
				return false, selection.Error("mismatch", "班级报告框架不唯一")
			}
			if len(found) == 1 {
				report = found[0]
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return selection.Candidate{}, err
		}
	}
	var matched matchedRow
	err = s.poll(ctx, func() (bool, error) {
		rows, err := readRows(report)
		if err != nil {
			return false, operationError(ctx, err)
		}
		if len(rows) == 0 {
			return false, nil
		}
		// Loading may render only a header before the data rows arrive.
		hasData := false
		for _, r := range rows {
			if r.class() != "" && r.class() != "上课班号" {
				hasData = true
			}
		}
		if !hasData {
			return false, nil
		}
		matched, err = matchClass(rows, s.target, s.target.Type == "public")
		return err == nil, err
	})
	if err != nil {
		return selection.Candidate{}, err
	}
	matched.Term = s.term
	s.current = matched
	s.classFrame = report
	if s.term == "" {
		return matched.Candidate, selection.Error("mismatch", "已找到班级，但无法识别目标选课学期；请人工核对页面，尚未提交")
	}
	if err = s.fault(); err != nil {
		return matched.Candidate, err
	}
	return matched.Candidate, nil
}

func (s *Session) Submit(ctx context.Context) error {
	if s.opts.DryRun {
		return selection.PreSubmitError("mismatch", "只查询演练禁止提交选课")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.classFrame == nil || s.term == "" {
		return selection.PreSubmitError("mismatch", "尚未完成课程和学期定位")
	}
	// Re-read immediately before clicking: row ordering and capacity may change.
	rows, err := readRows(s.classFrame)
	if err != nil {
		return operationError(ctx, err)
	}
	row, err := matchClass(rows, s.target, s.target.Type == "public")
	if err != nil {
		return selection.PreSubmitError(selection.Code(err), err.Error())
	}
	if row.Remaining == 0 {
		return selection.PreSubmitError("full", "目标班级已满")
	}
	s.clearFault()
	loc := s.classFrame.Locator("tr").Nth(row.Index)
	var clickErr error
	if s.target.Type == "public" {
		button := loc.Locator("[id$='_xz'] a")
		if n, _ := button.Count(); n != 1 {
			return selection.PreSubmitError("mismatch", "选课提交控件不唯一")
		}
		s.beginSubmission()
		defer s.endSubmission()
		clickErr = button.Click()
	} else {
		if s.detailFrame == nil {
			return selection.PreSubmitError("mismatch", "班级详情已失效")
		}
		input := loc.Locator("[id$='_ischk'] input")
		if n, _ := input.Count(); n != 1 {
			return selection.PreSubmitError("mismatch", "目标班级选择控件不唯一")
		}
		checked, checkErr := input.IsChecked()
		if checkErr != nil {
			return selection.PreSubmitError("mismatch", "无法确认目标班级勾选状态")
		}
		checkedCount, countErr := s.classFrame.Locator("input[type='checkbox']:checked").Count()
		if countErr != nil || checkedCount > 1 || (checkedCount == 1 && !checked) {
			return selection.PreSubmitError("mismatch", "班级页面已有其他选项被勾选，已停止以避免误选")
		}
		if !checked {
			if err = input.Check(); err != nil {
				return selection.PreSubmitError("mismatch", "无法安全勾选目标班级")
			}
		}
		s.beginSubmission()
		defer s.endSubmission()
		clickErr = s.detailFrame.Locator("#btnSubmit").Click()
	}
	settleErr := s.waitSubmission(ctx)
	if settleErr != nil {
		return settleErr
	}
	return operationError(ctx, clickErr)
}

// Verify uses the site's existing read-only result menu. No guessed endpoints
// or dates are substituted when the current page lacks semester evidence.
func (s *Session) Verify(ctx context.Context) (bool, error) {
	if s.opts.DryRun {
		return false, selection.Error("mismatch", "演练不执行提交后核验")
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if visible(s.page.Locator("#password-input")) {
		return false, selection.Error("auth_required", "核验时登录已失效")
	}
	menu := s.page.Locator("#JW130404")
	if !visible(menu) {
		menu = s.page.GetByText("选课结果", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})
	}
	if n, _ := menu.Count(); n != 1 || !visible(menu) {
		return false, selection.Error("unknown", "未找到唯一的选课结果入口，请手动核对")
	}
	// Clear old submit dialog errors; they must not short-circuit verification.
	s.clearFault()
	resp, err := s.page.ExpectResponse(func(url string) bool {
		return strings.Contains(url, "wsxk.zxjg")
	}, func() error { return menu.Click() }, playwright.PageExpectResponseOptions{Timeout: playwright.Float(float64(s.opts.Timeout.Milliseconds()))})
	if err != nil {
		return false, selection.Error("unknown", "选课结果页面未加载，需人工核对")
	}
	if resp.Status() != 200 {
		return false, selection.Error("unknown", "选课结果查询失败，需人工核对")
	}
	if resp.Request().ResourceType() != "document" {
		return false, selection.Error("unknown", "选课结果入口返回了非页面响应，需人工核对")
	}
	frame := resp.Frame()
	var resultTerm string
	var rows []tableRow
	if err = s.poll(ctx, func() (bool, error) {
		resultTerm = readTerm(frame)
		if resultTerm == "" {
			return false, nil
		}
		var readErr error
		rows, readErr = readRows(frame)
		if readErr != nil {
			return false, operationError(ctx, readErr)
		}
		return len(rows) > 0, nil
	}); err != nil {
		return false, selection.Error("unknown", "选课结果加载未完成")
	}
	if s.term == "" || resultTerm != s.term {
		return false, selection.Error("unknown", "选课结果学期与目标学期无法核对，需人工确认")
	}
	count := 0
	recognized := false
	for _, r := range rows {
		// Verification requires named columns, not any coincidental cell text.
		if r.course() != "" && r.class() != "" {
			recognized = true
		}
		if r.matchesSelectedCourse(s.target) && r.class() == s.target.ClassID {
			count++
		}
	}
	if !recognized || count > 1 {
		return false, selection.Error("unknown", "选课结果表格无法唯一核验，需人工确认")
	}
	return count == 1, nil
}
