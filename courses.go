package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/portal"
	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type CourseRequest struct {
	Mode      string             `json:"mode"`
	Speed     int                `json:"speed"`
	StudentID string             `json:"studentID"`
	Password  string             `json:"password"`
	Courses   []selection.Target `json:"courses"`
	Headless  bool               `json:"headless"`
	UseWebVPN bool               `json:"useWebVpn"`
}

func (a *App) StartCourses(req CourseRequest) error    { return a.runCourses(req, false) }
func (a *App) RehearseCourses(req CourseRequest) error { return a.runCourses(req, true) }

func (a *App) StopCourses() error {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

func validateRequest(req CourseRequest) (CourseRequest, error) {
	var err error
	req.Courses, err = selection.Normalize(req.Courses)
	if err != nil {
		return req, err
	}
	req.StudentID = strings.TrimSpace(req.StudentID)
	if req.StudentID == "" || req.Password == "" {
		return req, errors.New("请输入学号和密码")
	}
	if utf8.RuneCountInString(req.StudentID) > 64 || strings.IndexFunc(req.StudentID, unicode.IsControl) >= 0 || utf8.RuneCountInString(req.Password) > 1024 {
		return req, errors.New("学号或密码格式无效")
	}
	if req.Speed < 500 || req.Speed > 5000 {
		return req, errors.New("刷新间隔应在 500 至 5000 毫秒之间")
	}
	if req.Mode != "CatchCourse" && req.Mode != "WatchCourse" && req.Mode != "WatchCourseSync" {
		return req, errors.New("未知选课模式")
	}
	return req, nil
}

func (a *App) runCourses(req CourseRequest, dry bool) error {
	req, err := validateRequest(req)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("已有任务运行，请先停止或等待结束")
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	a.cancel, a.done = cancel, done
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.cancel = nil
		a.done = nil
		close(done)
		a.mu.Unlock()
	}()
	// Installation is an explicit startup preparation, never a retry side effect.
	a.installMu.Lock()
	ready := a.browserReady
	browserPath := a.browserPath
	a.installMu.Unlock()
	if !ready {
		return errors.New("请先等待浏览器安装完成")
	}
	if ctx.Err() != nil {
		return nil
	}
	browser, err := portal.OpenContext(ctx, portal.Options{StudentID: req.StudentID, Password: req.Password,
		Headless: req.Headless, UseWebVPN: req.UseWebVPN, DryRun: dry, ExecutablePath: browserPath})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	defer browser.Close()
	runner := selection.Runner{NewSession: browser.NewSession, Emit: func(e selection.Event) {
		if a.ctx == nil {
			return
		}
		runtime.EventsEmit(a.ctx, "courseStatus", e)
		message := fmt.Sprintf("%s / %s：%s", e.CourseID, e.ClassID, e.Message)
		runtime.EventsEmit(a.ctx, "currentStatus", message)
		switch e.State {
		case "selected", "ready", "full", "unknown", "mismatch", "auth_required", "conflict", "ineligible", "failed":
			runtime.EventsEmit(a.ctx, "importantStatus", message)
		}
	}}
	err = runner.Run(ctx, selection.Request{Mode: req.Mode, DryRun: dry, Interval: time.Duration(req.Speed) * time.Millisecond, Targets: req.Courses})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func legacyRequest(mode, kind string, speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) (CourseRequest, error) {
	if len(courseID) != len(classID) {
		return CourseRequest{}, errors.New("课程代码与班号数量不一致")
	}
	req := CourseRequest{Mode: mode, Speed: speed, StudentID: studentID, Password: password, Headless: headless, UseWebVPN: useWebVPN}
	for i := range courseID {
		req.Courses = append(req.Courses, selection.Target{Type: kind, CourseID: courseID[i], ClassID: classID[i]})
	}
	return req, nil
}

func (a *App) legacyRun(mode, kind string, speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	req, err := legacyRequest(mode, kind, speed, studentID, password, courseID, classID, headless, useWebVPN)
	if err != nil {
		return err
	}
	return a.StartCourses(req)
}
