package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"github.com/mxschmitt/playwright-go"
)

// These fixtures use only loopback HTTP. Enrollment counters prove that query
// and dry-run paths have no write side effects, even with real browser clicks.
type portalFixture struct {
	kind                                                                 string
	reordered, delayed, duplicate, loginExpired, selected, differentTerm bool
	full                                                                 bool
	dialog                                                               string
	enrollDelay                                                          time.Duration
	otherChecked                                                         bool
	enrollments                                                          atomic.Int32
	resultQueries                                                        atomic.Int32
}

func (f *portalFixture) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch r.URL.Path {
	case "/":
		if f.loginExpired {
			fmt.Fprint(w, `<input id="password-input" type="password">`)
			return
		}
		form := `<iframe name="frmDesk" src="/form" style="width:900px;height:650px"></iframe>`
		noise := `<iframe name="noiseA" src="/noise"></iframe><iframe name="noiseB" src="/noise"></iframe>`
		if f.reordered {
			form = noise + form
		} else {
			form += noise
		}
		fmt.Fprint(w, `<button id="JW130403">专业课</button><button id="JW130415">公选课</button><a id="JW130404" href="/wsxk.zxjg" target="resultFrame">选课结果</a>`+form+`<iframe name="resultFrame"></iframe>`)
	case "/noise":
		fmt.Fprint(w, "Unrelated frame")
	case "/form":
		fmt.Fprint(w, `<p>学年学期：2026-2027 秋季学期</p><form action="/report" target="frmReport"><input id="kcmc" name="course"><input id="t_skbh" name="class"><input type="checkbox" id="kkdw_range_all"><button id="btnQry">查询</button></form><iframe name="frmReport" id="frmReport" style="width:800px;height:450px"></iframe>`)
	case "/report":
		if f.delayed {
			time.Sleep(100 * time.Millisecond)
		}
		if f.kind == "major" {
			fmt.Fprint(w, `<table><tr><th>课程代码</th><th>操作</th></tr><tr><td>PHY001</td><td id="tr0_operation"><a href="/detail" target="_parent">选择班级</a></td></tr><tr><td>PHY0010</td><td>相似课程</td></tr></table>`)
			return
		}
		f.writeClassTable(w, false)
	case "/detail":
		fmt.Fprint(w, `<button id="btnSubmit" onclick="enroll()">提交选课</button><iframe id="frmReport" name="frmReport" src="/classes" style="width:800px;height:400px"></iframe>`+f.enrollScript())
	case "/classes":
		f.writeClassTable(w, true)
	case "/enroll":
		if f.enrollDelay > 0 {
			time.Sleep(f.enrollDelay)
		}
		f.enrollments.Add(1)
		fmt.Fprint(w, "ok")
	case "/wsxk.zxjg":
		f.resultQueries.Add(1)
		term := "2026-2027 秋季学期"
		if f.differentTerm {
			term = "2025-2026 秋季学期"
		}
		fmt.Fprintf(w, `<p>学年学期：%s</p><table><tr><th>课程代码</th><th>上课班号</th></tr>`, term)
		if f.selected {
			fmt.Fprint(w, `<tr><td> PHY001 </td><td> 01 </td></tr>`)
		} else {
			fmt.Fprint(w, `<tr><td>OTHER</td><td>02</td></tr>`)
		}
		fmt.Fprint(w, `</table>`)
	default:
		http.NotFound(w, r)
	}
}
func (f *portalFixture) enrollScript() string {
	return fmt.Sprintf(`<script>function enroll(){fetch('/enroll',{method:'POST'}).then(()=>{%s})}</script>`, func() string {
		if f.dialog != "" {
			return fmt.Sprintf("alert(%q);", f.dialog)
		}
		return ""
	}())
}
func (f *portalFixture) writeClassTable(w http.ResponseWriter, major bool) {
	remaining := 3
	if f.full {
		remaining = 0
	}
	header := `<tr><th>课程代码</th><th>上课班号</th><th>可选人数</th><th>课程名称</th><th>操作</th></tr>`
	row := func(n int, code, class string) string {
		action := fmt.Sprintf(`<td id="tr%d_xz"><a href="#" onclick="enroll();return false">选课</a></td>`, n)
		if major {
			checked := ""
			if f.otherChecked && n == 1 {
				checked = " checked"
			}
			action = fmt.Sprintf(`<td id="tr%d_ischk"><input type="checkbox"%s></td>`, n, checked)
		}
		return fmt.Sprintf(`<tr><td id="tr%d_kcdm">%s</td><td id="tr%d_curent_skbjdm"> %s </td><td id="tr%d_kxrs">%d</td><td id="tr%d_kcmc">力学</td>%s</tr>`, n, code, n, class, n, remaining, n, action)
	}
	rows := row(0, "PHY001", "01") + row(1, "PHY001", "1")
	if !major {
		rows += row(2, "PHY0010", "01")
	}
	if f.duplicate {
		rows += row(3, "PHY001", "01")
	}
	fmt.Fprint(w, f.enrollScript())
	if f.delayed {
		fmt.Fprintf(w, `<table id="results">%s</table><script>setTimeout(()=>document.querySelector('#results').insertAdjacentHTML('beforeend',%q),120)</script>`, header, rows)
	} else {
		fmt.Fprint(w, "<table>"+header+rows+"</table>")
	}
}
func fixtureSession(t *testing.T, browser playwright.Browser, f *portalFixture, dry bool) *Session {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(server.Close)
	bc, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{bc: bc, target: selection.Target{Type: f.kind, CourseID: "PHY001", ClassID: "01"}, opts: Options{DryRun: dry, Timeout: 2 * time.Second}, initialized: true, done: make(chan struct{})}
	page, err := bc.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	s.page = page
	s.configurePage(page)
	t.Cleanup(func() { _ = s.Close() })
	if _, err = page.Goto(server.URL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad}); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestBrowserIntegration(t *testing.T) {
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("Playwright driver required: %v", err)
	}
	defer pw.Stop()
	launchOptions := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)}
	if executable := os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE"); executable != "" {
		launchOptions.ExecutablePath = playwright.String(executable)
	}
	browser, err := pw.Chromium.Launch(launchOptions)
	if err != nil {
		t.Fatalf("Chromium required; install with go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install chromium: %v", err)
	}
	defer browser.Close()
	for _, kind := range []string{"public", "major"} {
		for _, reorder := range []bool{false, true} {
			t.Run(fmt.Sprintf("query_%s_reordered_%v", kind, reorder), func(t *testing.T) {
				f := &portalFixture{kind: kind, reordered: reorder, delayed: true}
				s := fixtureSession(t, browser, f, false)
				c, err := s.Query(context.Background())
				if err != nil || c.Remaining != 3 || c.Name != "力学" || c.Term != "2026-2027/秋" {
					t.Fatalf("query %+v %v", c, err)
				}
				if f.enrollments.Load() != 0 {
					t.Fatal("query enrolled student")
				}
			})
		}
	}
	for _, kind := range []string{"public", "major"} {
		t.Run("reject_duplicate_"+kind, func(t *testing.T) {
			f := &portalFixture{kind: kind, duplicate: true}
			s := fixtureSession(t, browser, f, false)
			_, err := s.Query(context.Background())
			if selection.Code(err) != "mismatch" || f.enrollments.Load() != 0 {
				t.Fatalf("ambiguous query %v submissions %d", err, f.enrollments.Load())
			}
		})
	}
	for _, kind := range []string{"public", "major"} {
		t.Run("full_"+kind, func(t *testing.T) {
			f := &portalFixture{kind: kind, full: true}
			s := fixtureSession(t, browser, f, false)
			c, err := s.Query(context.Background())
			if err != nil || c.Remaining != 0 {
				t.Fatalf("full query %+v %v", c, err)
			}
			if err = s.Submit(context.Background()); selection.Code(err) != "full" || f.enrollments.Load() != 0 {
				t.Fatalf("full submission %v", err)
			}
		})
	}
	for _, tc := range []struct{ name, dialog, code string }{{"conflict", "课程时间冲突", "conflict"}, {"ineligible", "不符合选课资格", "ineligible"}, {"full", "班级人数已满", "full"}} {
		t.Run("submit_"+tc.name, func(t *testing.T) {
			f := &portalFixture{kind: "public", dialog: tc.dialog}
			s := fixtureSession(t, browser, f, false)
			if _, err := s.Query(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.Submit(context.Background()); selection.Code(err) != tc.code {
				t.Fatalf("want %s got %v", tc.code, err)
			}
			ok, err := s.Verify(context.Background())
			if ok || err != nil || f.enrollments.Load() != 1 || f.resultQueries.Load() != 1 {
				t.Fatalf("verification %v %v writes %d", ok, err, f.enrollments.Load())
			}
		})
	}
	for _, tc := range []struct {
		name                    string
		selected, differentTerm bool
	}{{"selected", true, false}, {"click_did_not_enroll", false, false}, {"wrong_term", true, true}} {
		t.Run("verify_"+tc.name, func(t *testing.T) {
			f := &portalFixture{kind: "major", selected: tc.selected, differentTerm: tc.differentTerm}
			s := fixtureSession(t, browser, f, false)
			if _, err := s.Query(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.Submit(context.Background()); err != nil {
				t.Fatal(err)
			}
			ok, err := s.Verify(context.Background())
			if tc.differentTerm {
				if ok || selection.Code(err) != "unknown" {
					t.Fatalf("wrong term accepted %v %v", ok, err)
				}
			} else if ok != tc.selected || err != nil {
				t.Fatalf("verify %v %v", ok, err)
			}
			if f.enrollments.Load() != 1 {
				t.Fatalf("writes %d", f.enrollments.Load())
			}
		})
	}
	t.Run("expired_login", func(t *testing.T) {
		f := &portalFixture{kind: "public", loginExpired: true}
		s := fixtureSession(t, browser, f, false)
		if _, err := s.Query(context.Background()); selection.Code(err) != "auth_required" {
			t.Fatalf("expired login %v", err)
		}
		if f.enrollments.Load() != 0 {
			t.Fatal("expired login submitted")
		}
	})
	for _, kind := range []string{"public", "major"} {
		t.Run("dry_run_"+kind, func(t *testing.T) {
			f := &portalFixture{kind: kind}
			s := fixtureSession(t, browser, f, true)
			states := []string{}
			r := selection.Runner{NewSession: func(context.Context, selection.Target) (selection.Session, error) { return s, nil }, Emit: func(e selection.Event) { states = append(states, e.State) }}
			err := r.Run(context.Background(), selection.Request{Mode: "CatchCourse", DryRun: true, Interval: time.Second, Targets: []selection.Target{s.target}})
			if err != nil {
				t.Fatal(err)
			}
			if f.enrollments.Load() != 0 || f.resultQueries.Load() != 0 || !strings.Contains(strings.Join(states, ","), "ready") {
				t.Fatalf("dry run states %v enrollments %d result queries %d", states, f.enrollments.Load(), f.resultQueries.Load())
			}
		})
	}
	t.Run("missing_term_blocks_submission", func(t *testing.T) {
		f := &portalFixture{kind: "public"}
		s := fixtureSession(t, browser, f, false)
		frame, err := s.findFrame(context.Background(), "#kcmc", "#btnQry")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = frame.Evaluate(`() => document.querySelector('p').remove()`); err != nil {
			t.Fatal(err)
		}
		_, err = s.Query(context.Background())
		if selection.Code(err) != "mismatch" {
			t.Fatalf("missing term accepted: %v", err)
		}
		if err = s.Submit(context.Background()); err == nil || f.enrollments.Load() != 0 {
			t.Fatalf("missing term submission %v", err)
		}
	})
	t.Run("duplicate_form_frames_rejected", func(t *testing.T) {
		f := &portalFixture{kind: "public"}
		s := fixtureSession(t, browser, f, false)
		if _, err := s.page.Evaluate(`() => new Promise(resolve=>{const f=document.createElement('iframe');f.src='/form';f.onload=resolve;document.body.append(f)})`); err != nil {
			t.Fatal(err)
		}
		_, err := s.Query(context.Background())
		if selection.Code(err) != "mismatch" || f.enrollments.Load() != 0 {
			t.Fatalf("ambiguous frame accepted %v", err)
		}
	})
	t.Run("capacity_rechecked_before_submission", func(t *testing.T) {
		f := &portalFixture{kind: "major"}
		s := fixtureSession(t, browser, f, false)
		if _, err := s.Query(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.classFrame.Evaluate(`() => document.querySelector('#tr0_kxrs').textContent='0'`); err != nil {
			t.Fatal(err)
		}
		if err := s.Submit(context.Background()); selection.Code(err) != "full" || f.enrollments.Load() != 0 {
			t.Fatalf("capacity change ignored %v", err)
		}
	})
	t.Run("submission_waits_for_async_request", func(t *testing.T) {
		f := &portalFixture{kind: "public", enrollDelay: 500 * time.Millisecond}
		s := fixtureSession(t, browser, f, false)
		if _, err := s.Query(context.Background()); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if err := s.Submit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed < f.enrollDelay || f.enrollments.Load() != 1 {
			t.Fatalf("submission returned before request completed: %v writes %d", elapsed, f.enrollments.Load())
		}
	})
	t.Run("major_rejects_other_checked_class", func(t *testing.T) {
		f := &portalFixture{kind: "major", otherChecked: true}
		s := fixtureSession(t, browser, f, false)
		if _, err := s.Query(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := s.Submit(context.Background()); !selection.IsPreSubmit(err) || selection.Code(err) != "mismatch" {
			t.Fatalf("unsafe checkbox state accepted: %v", err)
		}
		if f.enrollments.Load() != 0 || f.resultQueries.Load() != 0 {
			t.Fatalf("unsafe state caused side effects: enroll %d verify %d", f.enrollments.Load(), f.resultQueries.Load())
		}
	})
	t.Run("direct_dry_submit_blocked", func(t *testing.T) {
		f := &portalFixture{kind: "public"}
		s := fixtureSession(t, browser, f, true)
		if _, err := s.Query(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := s.Submit(context.Background()); err == nil {
			t.Fatal("direct dry-run Submit accepted")
		}
		if f.enrollments.Load() != 0 {
			t.Fatal("dry-run submit sent request")
		}
	})
}
