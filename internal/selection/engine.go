package selection

import (
	"context"
	"errors"
	"sync"
	"time"
)

func Sleep(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *Runner) wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Wait != nil {
		return r.Wait(ctx, delay)
	}
	return Sleep(ctx, delay)
}

func (r *Runner) event(t Target, c Candidate, state, message string) {
	if r.Emit != nil {
		r.Emit(Event{Target: t, Candidate: c, Key: t.Key(), State: state, Message: message})
	}
}

func (r *Runner) retry(ctx context.Context, target Target, fn func() error) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn()
		if Code(err) != "transient" || attempt == 5 {
			return err
		}
		r.event(target, Candidate{}, "waiting", "临时网络错误，等待后重试查询")
		if err := r.wait(ctx, time.Second<<attempt); err != nil {
			return err
		}
	}
}

type task struct {
	target  Target
	session Session
	done    bool
}

// step never repeats a submission. An ambiguous outcome is terminal, including
// cancellation while a submission may already be in flight.
func (r *Runner) step(ctx context.Context, req Request, t *task) (selected bool, err error) {
	c := Candidate{}
	fail := func(err error) (bool, error) {
		t.done = true
		r.event(t.target, c, Code(err), err.Error())
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if t.session == nil {
		err := r.retry(ctx, t.target, func() error {
			var err error
			t.session, err = r.NewSession(ctx, t.target)
			if err != nil && t.session != nil {
				_ = t.session.Close()
				t.session = nil
			}
			return err
		})
		if err != nil {
			return fail(err)
		}
	}
	r.event(t.target, c, "querying", "查询目标课程和班级")
	err = r.retry(ctx, t.target, func() error { var e error; c, e = t.session.Query(ctx); return e })
	if err != nil {
		if Code(err) == "not_open" && req.Mode == "CatchCourse" && !req.DryRun {
			r.event(t.target, c, "waiting", "未到选课时间，等待下一次查询")
			return false, nil
		}
		return fail(err)
	}
	if c.Remaining == 0 {
		r.event(t.target, c, "full", "目标班级可选人数为零")
		t.done = req.DryRun || req.Mode == "CatchCourse"
		return false, nil
	}
	if req.DryRun {
		t.done = true
		r.event(t.target, c, "ready", "只查询演练完成；未提交选课")
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	r.event(t.target, c, "submitted", "提交中，结果待核验")
	submitErr := t.session.Submit(ctx)
	// A click timeout is not evidence that the server rejected the selection.
	ok, verifyErr := t.session.Verify(ctx)
	t.done = true
	if ok && verifyErr == nil {
		r.event(t.target, c, "selected", "已在目标学期选课结果中核验：已选上")
		return true, nil
	}
	if verifyErr == nil {
		switch Code(submitErr) {
		case "full":
			r.event(t.target, c, "full", submitErr.Error())
			return false, nil
		case "conflict", "ineligible":
			return fail(submitErr)
		}
	}
	return fail(Error("unknown", "提交结果无法核验，已暂停该课程；请到教务系统核对，勿盲目重试"))
}

// Run owns every session until all workers have exited and been cleaned up.
// The synchronous watch mode rotates courses and stops after its first success.
func (r *Runner) Run(ctx context.Context, req Request) error {
	targets, err := Normalize(req.Targets)
	if err != nil {
		return err
	}
	if req.Mode != "CatchCourse" && req.Mode != "WatchCourse" && req.Mode != "WatchCourseSync" {
		return errors.New("未知选课模式")
	}
	if req.Interval <= 0 {
		return errors.New("刷新间隔必须大于零")
	}
	if r.NewSession == nil {
		return errors.New("缺少浏览器会话")
	}
	tasks := make([]task, len(targets))
	for i, t := range targets {
		tasks[i].target = t
		r.event(t, Candidate{}, "pending", "等待查询")
	}
	var errs []error
	if req.Mode == "WatchCourseSync" {
		defer func() {
			for i := range tasks {
				if tasks[i].session != nil {
					_ = tasks[i].session.Close()
				}
			}
		}()
		for {
			pending := false
			for i := range tasks {
				if tasks[i].done {
					continue
				}
				selected, err := r.step(ctx, req, &tasks[i])
				if err != nil {
					errs = append(errs, err)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if selected {
					for j := range tasks {
						if !tasks[j].done {
							r.event(tasks[j].target, Candidate{}, "cancelled", "单线程模式已有一门选上，结束其余课程")
						}
					}
					return errors.Join(errs...)
				}
				pending = pending || !tasks[i].done
			}
			if !pending {
				break
			}
			if err := r.wait(ctx, req.Interval); err != nil {
				return err
			}
		}
	} else {
		var wg sync.WaitGroup
		results := make(chan error, len(tasks))
		for i := range tasks {
			wg.Add(1)
			go func(t *task) {
				defer wg.Done()
				defer func() {
					if t.session != nil {
						_ = t.session.Close()
					}
				}()
				for {
					_, err := r.step(ctx, req, t)
					if err != nil || t.done {
						results <- err
						return
					}
					if err := r.wait(ctx, req.Interval); err != nil {
						r.event(t.target, Candidate{}, "cancelled", "任务已停止")
						results <- err
						return
					}
				}
			}(&tasks[i])
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(errs...)
}
