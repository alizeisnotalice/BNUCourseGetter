package portal

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"github.com/mxschmitt/playwright-go"
)

type Options struct {
	StudentID, Password         string
	ExecutablePath              string
	Headless, UseWebVPN, DryRun bool
	Timeout                     time.Duration
}

type Browser struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	opts    Options
}

func Open(opts Options) (*Browser, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}
	pw, err := playwright.Run()
	if err != nil {
		return nil, selection.Error("failed", "无法启动浏览器驱动，请先完成浏览器安装")
	}
	launchOptions := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(opts.Headless && !opts.DryRun)}
	if opts.ExecutablePath != "" {
		launchOptions.ExecutablePath = playwright.String(opts.ExecutablePath)
	}
	browser, err := pw.Chromium.Launch(launchOptions)
	if err != nil {
		_ = pw.Stop()
		return nil, selection.Error("failed", "无法启动 Chromium，请检查浏览器安装")
	}
	return &Browser{pw: pw, browser: browser, opts: opts}, nil
}

func (b *Browser) Close() { _ = b.browser.Close(); _ = b.pw.Stop() }

func (b *Browser) NewSession(ctx context.Context, target selection.Target) (selection.Session, error) {
	bc, err := b.browser.NewContext()
	if err != nil {
		return nil, selection.Error("failed", "无法创建浏览器会话")
	}
	s := &Session{bc: bc, target: target, opts: b.opts, done: make(chan struct{})}
	// Install the handler on every page, including WebVPN popups.
	bc.OnPage(func(p playwright.Page) { s.configurePage(p) })
	page, err := bc.NewPage()
	if err != nil {
		_ = s.Close()
		return nil, selection.Error("failed", "无法创建浏览器页面")
	}
	s.page = page
	s.configurePage(page)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()
	return s, nil
}

type Session struct {
	bc           playwright.BrowserContext
	page         playwright.Page
	target       selection.Target
	opts         Options
	initialized  bool
	term         string
	classFrame   playwright.Frame
	detailFrame  playwright.Frame
	current      matchedRow
	submitting   atomic.Bool
	mu           sync.Mutex
	dialogErr    error
	configured   map[playwright.Page]struct{}
	submitReqs   map[playwright.Request]struct{}
	submitNotify chan struct{}
	closeOnce    sync.Once
	done         chan struct{}
}

func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.bc.Close(); close(s.done) })
	return err
}
func (s *Session) configurePage(p playwright.Page) {
	s.mu.Lock()
	if s.configured == nil {
		s.configured = make(map[playwright.Page]struct{})
	}
	if _, ok := s.configured[p]; ok {
		s.mu.Unlock()
		return
	}
	s.configured[p] = struct{}{}
	if s.submitNotify == nil {
		s.submitNotify = make(chan struct{}, 1)
	}
	s.mu.Unlock()
	p.SetDefaultTimeout(float64(s.opts.Timeout.Milliseconds()))
	p.SetDefaultNavigationTimeout(float64(s.opts.Timeout.Milliseconds()))
	p.OnRequest(func(r playwright.Request) {
		if !s.submitting.Load() || !isSubmissionResource(r.ResourceType()) {
			return
		}
		s.mu.Lock()
		if s.submitReqs != nil {
			s.submitReqs[r] = struct{}{}
			s.signalSubmitLocked()
		}
		s.mu.Unlock()
	})
	finishRequest := func(r playwright.Request) {
		s.mu.Lock()
		if _, ok := s.submitReqs[r]; ok {
			delete(s.submitReqs, r)
			s.signalSubmitLocked()
		}
		s.mu.Unlock()
	}
	p.OnRequestFinished(finishRequest)
	p.OnRequestFailed(finishRequest)
	p.OnDialog(func(d playwright.Dialog) {
		message := d.Message()
		fault := businessError(message)
		accept := false
		if s.submitting.Load() && d.Type() == "confirm" && fault == nil &&
			!strings.Contains(message, "退") && !strings.Contains(message, "替换") && !strings.Contains(message, "删除") {
			accept = strings.Contains(message, "选课") || strings.Contains(message, "选择此") || strings.Contains(message, "选择该") || strings.Contains(message, "选修")
		}
		if fault == nil && !accept && !strings.Contains(message, "成功") {
			fault = selection.Error("mismatch", "出现未识别的提示，已停止自动操作，请查看教务页面")
		}
		s.mu.Lock()
		if fault != nil {
			s.dialogErr = fault
		}
		s.signalSubmitLocked()
		s.mu.Unlock()
		if accept {
			_ = d.Accept()
		} else {
			_ = d.Dismiss()
		}
	})
}

func isSubmissionResource(kind string) bool {
	return kind == "document" || kind == "xhr" || kind == "fetch"
}

func (s *Session) signalSubmitLocked() {
	if s.submitNotify == nil {
		return
	}
	select {
	case s.submitNotify <- struct{}{}:
	default:
	}
}

func (s *Session) beginSubmission() {
	s.mu.Lock()
	s.submitReqs = make(map[playwright.Request]struct{})
	if s.submitNotify == nil {
		s.submitNotify = make(chan struct{}, 1)
	}
	for {
		select {
		case <-s.submitNotify:
		default:
			s.mu.Unlock()
			s.submitting.Store(true)
			return
		}
	}
}

func (s *Session) endSubmission() { s.submitting.Store(false) }

func (s *Session) waitSubmission(ctx context.Context) error {
	quiet := time.NewTimer(300 * time.Millisecond)
	deadline := time.NewTimer(s.opts.Timeout)
	defer quiet.Stop()
	defer deadline.Stop()
	resetQuiet := func() {
		if !quiet.Stop() {
			select {
			case <-quiet.C:
			default:
			}
		}
		quiet.Reset(300 * time.Millisecond)
	}
	for {
		if err := s.fault(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.submitNotify:
			resetQuiet()
		case <-quiet.C:
			s.mu.Lock()
			pending := len(s.submitReqs)
			s.mu.Unlock()
			if pending == 0 {
				return s.fault()
			}
			quiet.Reset(100 * time.Millisecond)
		case <-deadline.C:
			return selection.Error("submit_pending", "提交请求未在限定时间内结束")
		}
	}
}
func businessError(message string) error {
	switch {
	case strings.Contains(message, "密码") && (strings.Contains(message, "错误") || strings.Contains(message, "不正确")), strings.Contains(message, "验证码"), strings.Contains(message, "重新登录"), strings.Contains(message, "登录超时"):
		return selection.Error("auth_required", "登录失效、密码错误或需要验证码，请手动处理后重试")
	case strings.Contains(message, "冲突"):
		return selection.Error("conflict", "课程时间冲突，请核对课表")
	case strings.Contains(message, "已满"), strings.Contains(message, "人数已达"), strings.Contains(message, "可选人数为零"), strings.Contains(message, "没有名额"):
		return selection.Error("full", "目标班级已满")
	case strings.Contains(message, "不符合"), strings.Contains(message, "不允许"), strings.Contains(message, "学分上限"), strings.Contains(message, "先修"), strings.Contains(message, "资格"):
		return selection.Error("ineligible", "不满足选课条件，请查看教务系统提示")
	case strings.Contains(message, "选课时间") && (strings.Contains(message, "不是") || strings.Contains(message, "未到") || strings.Contains(message, "不在")):
		return selection.Error("not_open", "当前不在有效选课时间区段")
	}
	return nil
}
func (s *Session) fault() error { s.mu.Lock(); defer s.mu.Unlock(); return s.dialogErr }
func (s *Session) clearFault()  { s.mu.Lock(); s.dialogErr = nil; s.mu.Unlock() }

// Driver errors may contain input values and URLs. Return only fixed,
// actionable messages; classify only recognizable transport failures as retryable.
func operationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	m := err.Error()
	for _, marker := range []string{"net::ERR_CONNECTION", "net::ERR_NETWORK", "net::ERR_TIMED_OUT", "net::ERR_NAME_NOT_RESOLVED", "net::ERR_INTERNET_DISCONNECTED"} {
		if strings.Contains(m, marker) {
			return selection.Error("transient", "校园网络暂时不可用")
		}
	}
	return selection.Error("mismatch", "页面未按预期加载或控件已变化；请使用只查询演练检查")
}

func (s *Session) poll(ctx context.Context, fn func() (bool, error)) error {
	deadline := time.Now().Add(s.opts.Timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ok, err := fn()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if err := s.fault(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return selection.Error("mismatch", "等待页面控件超时，请检查登录状态或页面适配情况")
		}
		if err := selection.Sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

func visible(loc playwright.Locator) bool { yes, err := loc.IsVisible(); return err == nil && yes }

func (s *Session) login(ctx context.Context) error {
	if s.initialized {
		if visible(s.page.Locator("#password-input")) || strings.Contains(s.page.URL(), "/cas/login") {
			return selection.Error("auth_required", "登录已失效，请重新开始并完成登录")
		}
		return nil
	}
	url := "https://cas.bnu.edu.cn/cas/login?service=http%3A%2F%2Fzyfw.bnu.edu.cn%2F"
	if s.opts.UseWebVPN {
		url = "https://one.bnu.edu.cn/dcp/forward.action?path=/portal/portal&p=home"
	}
	response, err := s.page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
	if err != nil {
		return operationError(ctx, err)
	}
	if response != nil && response.Status() >= 500 {
		return selection.Error("transient", "登录服务暂时不可用")
	}
	if err = s.page.Locator("#loginForm > div > div.username > input").Fill(s.opts.StudentID); err != nil {
		return operationError(ctx, err)
	}
	if err = s.page.Locator("#password-input").Fill(s.opts.Password); err != nil {
		return operationError(ctx, err)
	}
	if err = s.page.Locator("#loginForm > div.login-btn-row > a").Click(); err != nil {
		return operationError(ctx, err)
	}
	err = s.poll(ctx, func() (bool, error) {
		for _, sel := range []string{".error-tip", ".error-msg", "#msg", "#errorMsg"} {
			loc := s.page.Locator(sel)
			if visible(loc) {
				text, _ := loc.InnerText()
				if e := businessError(text); e != nil {
					return false, e
				}
			}
		}
		link := s.page.GetByText("继续访问原地址", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})
		if visible(link) {
			if err := link.Click(); err != nil {
				return false, operationError(ctx, err)
			}
		}
		return visible(s.page.Locator("li[data-code='JW1304']")) || (s.opts.UseWebVPN && visible(s.page.Locator("#recently_div"))), nil
	})
	if err != nil {
		if selection.Code(err) == "mismatch" {
			return selection.Error("auth_required", "登录未完成，请在浏览器检查密码、验证码或认证提示")
		}
		return err
	}
	if !visible(s.page.Locator("li[data-code='JW1304']")) && s.opts.UseWebVPN {
		items := s.page.Locator("#recently_div > ul > li").Filter(playwright.LocatorFilterOptions{HasText: "教务管理系统"})
		if n, _ := items.Count(); n != 1 {
			return selection.Error("mismatch", "数字京师中未找到唯一的教务管理系统入口，请先手动访问一次")
		}
		page, err := s.page.ExpectPopup(func() error { return items.Click() }, playwright.PageExpectPopupOptions{Timeout: playwright.Float(float64(s.opts.Timeout.Milliseconds()))})
		if err != nil {
			return operationError(ctx, err)
		}
		s.page = page
	}
	if err := s.poll(ctx, func() (bool, error) { return visible(s.page.Locator("li[data-code='JW1304']")), nil }); err != nil {
		return err
	}
	s.initialized = true
	return nil
}
