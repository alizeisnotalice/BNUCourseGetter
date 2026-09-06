package selection

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeSession struct {
	closeFn                            func() error
	query                              func(context.Context) (Candidate, error)
	submit                             func(context.Context) error
	verify                             func(context.Context) (bool, error)
	queries, submits, verifies, closes int
}

func (s *fakeSession) Query(ctx context.Context) (Candidate, error) {
	s.queries++
	if s.query != nil {
		return s.query(ctx)
	}
	return Candidate{Remaining: 1}, nil
}
func (s *fakeSession) Submit(ctx context.Context) error {
	s.submits++
	if s.submit != nil {
		return s.submit(ctx)
	}
	return nil
}
func (s *fakeSession) Verify(ctx context.Context) (bool, error) {
	s.verifies++
	if s.verify != nil {
		return s.verify(ctx)
	}
	return true, nil
}
func (s *fakeSession) Close() error {
	s.closes++
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) emit(e Event) { l.mu.Lock(); defer l.mu.Unlock(); l.events = append(l.events, e) }
func (l *eventLog) has(key, state string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.events {
		if e.Key == key && e.State == state {
			return true
		}
	}
	return false
}

var firstTarget = Target{Type: "public", CourseID: "PHY101", ClassID: "01"}
var secondTarget = Target{Type: "major", CourseID: "PHY102", ClassID: "02"}

func request(mode string) Request {
	return Request{Mode: mode, Interval: time.Second, Targets: []Target{firstTarget}}
}
func runnerFor(s *fakeSession, l *eventLog) *Runner {
	return &Runner{NewSession: func(context.Context, Target) (Session, error) { return s, nil }, Emit: l.emit, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
}

func TestNormalizePreservesClassIdentifierAndDeduplicates(t *testing.T) {
	got, err := Normalize([]Target{{Type: "major", CourseID: " PHY101 ", ClassID: " 01 "}, {Type: "major", CourseID: "PHY101", ClassID: "01"}})
	if err != nil || !reflect.DeepEqual(got, []Target{{Type: "major", CourseID: "PHY101", ClassID: "01"}}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, v := range [][]Target{nil, {{Type: "other", CourseID: "X", ClassID: "1"}}, {{Type: "public", CourseID: " ", ClassID: "1"}}, {{Type: "public", CourseID: "X", ClassID: " "}}, {{Type: "public", CourseID: "X|Y", ClassID: "1"}}, {{Type: "public", CourseID: "X", ClassID: "1\n2"}}} {
		if _, err := Normalize(v); err == nil {
			t.Errorf("accepted invalid targets %v", v)
		}
	}
}
func TestDryRunNeverSubmitsOrVerifies(t *testing.T) {
	for _, remaining := range []int{0, 1} {
		t.Run(string(rune('0'+remaining)), func(t *testing.T) {
			s := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{Name: "Physics", Remaining: remaining}, nil }}
			l := &eventLog{}
			req := request("CatchCourse")
			req.DryRun = true
			if err := runnerFor(s, l).Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			state := "ready"
			if remaining == 0 {
				state = "full"
			}
			if !l.has(firstTarget.Key(), state) || s.submits != 0 || s.verifies != 0 || s.closes != 1 {
				t.Fatalf("state/calls: %+v %+v", l.events, s)
			}
		})
	}
}
func TestSubmissionIsNeverReportedSelectedWithoutVerification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		submit   error
		verified bool
		verify   error
		state    string
	}{
		{"verified", nil, true, nil, "selected"}, {"unconfirmed", nil, false, nil, "unknown"}, {"verification timeout", nil, false, context.DeadlineExceeded, "unknown"}, {"submit timeout", context.DeadlineExceeded, false, nil, "unknown"}, {"submit timeout but selected", context.DeadlineExceeded, true, nil, "selected"}, {"conflict", Error("conflict", "conflict"), false, nil, "conflict"}, {"ineligible", Error("ineligible", "ineligible"), false, nil, "ineligible"}, {"full after submission", Error("full", "full"), false, nil, "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeSession{submit: func(context.Context) error { return tc.submit }, verify: func(context.Context) (bool, error) { return tc.verified, tc.verify }}
			l := &eventLog{}
			err := runnerFor(s, l).Run(context.Background(), request("WatchCourse"))
			if s.submits != 1 || s.verifies != 1 || s.closes != 1 || !l.has(firstTarget.Key(), tc.state) {
				t.Fatalf("calls/state: %+v %+v", s, l.events)
			}
			if (tc.state == "selected" || tc.state == "full") != (err == nil) {
				t.Fatalf("state %s error %v", tc.state, err)
			}
			if tc.state != "selected" && l.has(firstTarget.Key(), "selected") {
				t.Fatal("false success")
			}
			if !l.has(firstTarget.Key(), "submitted") {
				t.Fatalf("missing pending verification event: %+v", l.events)
			}
		})
	}
}
func TestTransientQueryRetriesAreBoundedAndBackOff(t *testing.T) {
	s := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{}, Error("transient", "network") }}
	l := &eventLog{}
	r := runnerFor(s, l)
	var waits []time.Duration
	r.Wait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if err := r.Run(context.Background(), request("CatchCourse")); err == nil {
		t.Fatal("expected exhausted retry error")
	}
	if s.queries != 6 || s.submits != 0 || s.closes != 1 || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}) {
		t.Fatalf("calls %+v waits %v", s, waits)
	}
}
func TestSessionCreationRetriesOnlyTransient(t *testing.T) {
	for _, code := range []string{"transient", "auth_required", "mismatch"} {
		t.Run(code, func(t *testing.T) {
			attempts := 0
			waits := 0
			r := &Runner{NewSession: func(context.Context, Target) (Session, error) { attempts++; return nil, Error(code, code) }, Wait: func(context.Context, time.Duration) error { waits++; return nil }}
			if err := r.Run(context.Background(), request("CatchCourse")); err == nil {
				t.Fatal("expected failure")
			}
			want := 1
			if code == "transient" {
				want = 6
			}
			if attempts != want || waits != want-1 {
				t.Fatalf("attempts %d waits %d", attempts, waits)
			}
		})
	}
}
func TestNonTransientQueryErrorsDoNotRetry(t *testing.T) {
	for _, code := range []string{"auth_required", "mismatch", "conflict", "ineligible"} {
		t.Run(code, func(t *testing.T) {
			s := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{}, Error(code, code) }}
			l := &eventLog{}
			if err := runnerFor(s, l).Run(context.Background(), request("WatchCourse")); err == nil {
				t.Fatal("expected failure")
			}
			if s.queries != 1 || s.submits != 0 || s.closes != 1 {
				t.Fatalf("unexpected retry: %+v", s)
			}
		})
	}
}
func TestFullCourseModesAndBrowserReuse(t *testing.T) {
	for _, mode := range []string{"CatchCourse", "WatchCourse", "WatchCourseSync"} {
		t.Run(mode, func(t *testing.T) {
			s := &fakeSession{}
			s.query = func(context.Context) (Candidate, error) {
				if s.queries == 1 {
					return Candidate{Remaining: 0}, nil
				}
				return Candidate{Remaining: 1}, nil
			}
			l := &eventLog{}
			r := runnerFor(s, l)
			created := 0
			r.NewSession = func(context.Context, Target) (Session, error) { created++; return s, nil }
			if err := r.Run(context.Background(), request(mode)); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode != "CatchCourse" {
				want = 2
			}
			if s.queries != want || created != 1 || s.closes != 1 {
				t.Fatalf("calls %+v creates %d", s, created)
			}
		})
	}
}
func TestNotOpenOnlyCatchWaits(t *testing.T) {
	for _, mode := range []string{"CatchCourse", "WatchCourse", "WatchCourseSync"} {
		t.Run(mode, func(t *testing.T) {
			s := &fakeSession{}
			s.query = func(context.Context) (Candidate, error) {
				if s.queries == 1 {
					return Candidate{}, Error("not_open", "closed")
				}
				return Candidate{Remaining: 1}, nil
			}
			r := runnerFor(s, &eventLog{})
			err := r.Run(context.Background(), request(mode))
			if mode == "CatchCourse" {
				if err != nil || s.queries != 2 || s.submits != 1 {
					t.Fatalf("catch did not wait: %+v %v", s, err)
				}
			} else if err == nil || s.queries != 1 || s.submits != 0 {
				t.Fatalf("watch retried closed selection: %+v %v", s, err)
			}
		})
	}
}
func TestSerialPollingDoesNotStarveLaterCourseAndStopsAfterSuccess(t *testing.T) {
	a := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{Remaining: 0}, nil }}
	b := &fakeSession{}
	sessions := map[string]*fakeSession{firstTarget.Key(): a, secondTarget.Key(): b}
	var order []string
	r := &Runner{NewSession: func(_ context.Context, target Target) (Session, error) {
		order = append(order, target.Key())
		return sessions[target.Key()], nil
	}, Wait: func(context.Context, time.Duration) error { return errors.New("unnecessary wait before second course") }}
	req := request("WatchCourseSync")
	req.Targets = append(req.Targets, secondTarget)
	if err := r.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if a.queries != 1 || a.submits != 0 || b.submits != 1 || a.closes != 1 || b.closes != 1 {
		t.Fatalf("a %+v b %+v order %v", a, b, order)
	}
}
func TestParallelCourseFailureDoesNotCancelOtherCourse(t *testing.T) {
	a := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{}, Error("mismatch", "bad page") }}
	b := &fakeSession{}
	l := &eventLog{}
	r := runnerFor(a, l)
	r.NewSession = func(_ context.Context, target Target) (Session, error) {
		if target == firstTarget {
			return a, nil
		}
		return b, nil
	}
	req := request("WatchCourse")
	req.Targets = append(req.Targets, secondTarget)
	if err := r.Run(context.Background(), req); err == nil {
		t.Fatal("failure must be returned")
	}
	if !l.has(secondTarget.Key(), "selected") || b.submits != 1 || a.closes != 1 || b.closes != 1 {
		t.Fatalf("a %+v b %+v events %+v", a, b, l.events)
	}
}
func TestCancellationWaitsForSessionCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	s := &fakeSession{query: func(ctx context.Context) (Candidate, error) {
		close(entered)
		<-ctx.Done()
		return Candidate{}, ctx.Err()
	}}
	r := runnerFor(s, &eventLog{})
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, request("WatchCourse")) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("query never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || s.closes != 1 || s.submits != 0 {
			t.Fatalf("err %v calls %+v", err, s)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
}
func TestCancellationInterruptsRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	s := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{}, Error("transient", "network") }}
	r := runnerFor(s, &eventLog{})
	r.Wait = func(ctx context.Context, _ time.Duration) error { close(waiting); <-ctx.Done(); return ctx.Err() }
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, request("CatchCourse")) }()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("retry never waited")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || s.queries != 1 || s.closes != 1 {
			t.Fatalf("err %v calls %+v", err, s)
		}
	case <-time.After(time.Second):
		t.Fatal("backoff ignored cancellation")
	}
}

func TestDryRunDoesNotWaitForOpening(t *testing.T) {
	s := &fakeSession{query: func(context.Context) (Candidate, error) { return Candidate{}, Error("not_open", "closed") }}
	r := runnerFor(s, &eventLog{})
	r.Wait = func(context.Context, time.Duration) error {
		t.Error("dry run waited for opening")
		return errors.New("stop")
	}
	req := request("CatchCourse")
	req.DryRun = true
	if err := r.Run(context.Background(), req); err == nil {
		t.Fatal("expected not open error")
	}
	if s.queries != 1 || s.submits != 0 || s.verifies != 0 || s.closes != 1 {
		t.Fatalf("unsafe dry run calls %+v", s)
	}
}
func TestNormalizedDuplicatesOnlySubmitOnce(t *testing.T) {
	s := &fakeSession{}
	l := &eventLog{}
	r := runnerFor(s, l)
	created := 0
	r.NewSession = func(_ context.Context, target Target) (Session, error) {
		created++
		if target != firstTarget {
			t.Errorf("target not normalized: %+v", target)
		}
		return s, nil
	}
	req := request("CatchCourse")
	req.Targets = append(req.Targets, Target{Type: "public", CourseID: " PHY101 ", ClassID: " 01 "})
	if err := r.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if created != 1 || s.submits != 1 || s.closes != 1 {
		t.Fatalf("duplicate submission: created %d calls %+v", created, s)
	}
}
func TestTransientQueryRecoverySubmitsOnce(t *testing.T) {
	s := &fakeSession{}
	s.query = func(context.Context) (Candidate, error) {
		if s.queries < 3 {
			return Candidate{}, Error("transient", "network")
		}
		return Candidate{Remaining: 1}, nil
	}
	l := &eventLog{}
	r := runnerFor(s, l)
	if err := r.Run(context.Background(), request("CatchCourse")); err != nil {
		t.Fatal(err)
	}
	if s.queries != 3 || s.submits != 1 || s.verifies != 1 || s.closes != 1 || !l.has(firstTarget.Key(), "selected") {
		t.Fatalf("recovery: %+v events %+v", s, l.events)
	}
}
func TestSuccessfulParallelCourseIsNotRepeatedWhileOtherCourseWaits(t *testing.T) {
	a := &fakeSession{}
	b := &fakeSession{}
	b.query = func(context.Context) (Candidate, error) {
		if b.queries < 3 {
			return Candidate{Remaining: 0}, nil
		}
		return Candidate{Remaining: 1}, nil
	}
	l := &eventLog{}
	r := runnerFor(a, l)
	r.NewSession = func(_ context.Context, target Target) (Session, error) {
		if target == firstTarget {
			return a, nil
		}
		return b, nil
	}
	req := request("WatchCourse")
	req.Targets = append(req.Targets, secondTarget)
	if err := r.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if a.queries != 1 || a.submits != 1 || b.queries != 3 || b.submits != 1 || a.closes != 1 || b.closes != 1 {
		t.Fatalf("a %+v b %+v", a, b)
	}
}
func TestRunnerRejectsInvalidRequestsWithoutCreatingSessions(t *testing.T) {
	for _, req := range []Request{{Mode: "CatchCourse"}, {Mode: "invalid", Targets: []Target{firstTarget}}, {Mode: "CatchCourse", Targets: []Target{{Type: "public", CourseID: "x", ClassID: ""}}}} {
		r := &Runner{NewSession: func(context.Context, Target) (Session, error) {
			t.Error("created session for invalid request")
			return &fakeSession{}, nil
		}}
		if err := r.Run(context.Background(), req); err == nil {
			t.Errorf("accepted invalid request %+v", req)
		}
	}
}

func TestRunDoesNotReturnBeforeCloseCompletes(t *testing.T) {
	closing := make(chan struct{})
	release := make(chan struct{})
	s := &fakeSession{closeFn: func() error { close(closing); <-release; return nil }}
	r := runnerFor(s, &eventLog{})
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), request("CatchCourse")) }()
	select {
	case <-closing:
	case <-time.After(time.Second):
		t.Fatal("close never started")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before cleanup: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not return after cleanup")
	}
}
func TestSerialFirstSuccessSkipsUnvisitedCourses(t *testing.T) {
	a := &fakeSession{}
	r := runnerFor(a, &eventLog{})
	r.NewSession = func(_ context.Context, target Target) (Session, error) {
		if target != firstTarget {
			t.Error("visited course after serial success")
		}
		return a, nil
	}
	req := request("WatchCourseSync")
	req.Targets = append(req.Targets, secondTarget)
	if err := r.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if a.submits != 1 || a.closes != 1 {
		t.Fatalf("unexpected repeats %+v", a)
	}
}
func TestCancellationDuringSubmitStillAttemptsVerificationWithoutResubmit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &fakeSession{submit: func(context.Context) error { cancel(); return context.Canceled }, verify: func(context.Context) (bool, error) { return false, context.Canceled }}
	l := &eventLog{}
	r := runnerFor(s, l)
	if err := r.Run(ctx, request("WatchCourse")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if s.submits != 1 || s.verifies != 1 || s.closes != 1 || !l.has(firstTarget.Key(), "unknown") {
		t.Fatalf("ambiguous cancel calls %+v events %+v", s, l.events)
	}
}
func TestPartiallyCreatedFailedSessionIsClosedBeforeRetry(t *testing.T) {
	broken := &fakeSession{}
	healthy := &fakeSession{}
	r := runnerFor(healthy, &eventLog{})
	created := 0
	r.NewSession = func(context.Context, Target) (Session, error) {
		created++
		if created == 1 {
			return broken, Error("transient", "connection reset")
		}
		if broken.closes != 1 {
			t.Error("failed session leaked before retry")
		}
		return healthy, nil
	}
	if err := r.Run(context.Background(), request("CatchCourse")); err != nil {
		t.Fatal(err)
	}
	if created != 2 || broken.closes != 1 || broken.queries != 0 || healthy.closes != 1 || healthy.submits != 1 {
		t.Fatalf("broken %+v healthy %+v", broken, healthy)
	}
}
