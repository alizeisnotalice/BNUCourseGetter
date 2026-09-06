package selection

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Target struct {
	Type     string `json:"type"`
	CourseID string `json:"courseID"`
	ClassID  string `json:"classID"`
}

func (t Target) Key() string { return t.Type + "|" + t.CourseID + "|" + t.ClassID }

func Normalize(targets []Target) ([]Target, error) {
	seen := map[string]bool{}
	result := make([]Target, 0, len(targets))
	for _, t := range targets {
		t.CourseID, t.ClassID = strings.TrimSpace(t.CourseID), strings.TrimSpace(t.ClassID)
		if (t.Type != "public" && t.Type != "major") || t.CourseID == "" || t.ClassID == "" || strings.ContainsAny(t.CourseID+t.ClassID, "|\r\n") {
			return nil, errors.New("课程类别、课程代码或上课班号无效")
		}
		if !seen[t.Key()] {
			result = append(result, t)
			seen[t.Key()] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("请添加课程")
	}
	return result, nil
}

type Candidate struct {
	Name      string `json:"name"`
	Teacher   string `json:"teacher"`
	Schedule  string `json:"schedule"`
	Term      string `json:"term"`
	Remaining int    `json:"remaining"`
}

type Event struct {
	Target
	Candidate
	Key     string `json:"key"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type Fault struct{ Code, Message string }

func (e *Fault) Error() string         { return e.Message }
func Error(code, message string) error { return &Fault{Code: code, Message: message} }
func Code(err error) string {
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "failed"
}

// A session owns one target and reuses its browser page across queries.
// Submit may have reached the server even when it returns an error.
type Session interface {
	Query(context.Context) (Candidate, error)
	Submit(context.Context) error
	Verify(context.Context) (bool, error)
	Close() error
}

type Request struct {
	Mode     string
	DryRun   bool
	Interval time.Duration
	Targets  []Target
}

type Runner struct {
	NewSession func(context.Context, Target) (Session, error)
	Emit       func(Event)
	// Tests inject a virtual clock; production uses cancellable timers.
	Wait func(context.Context, time.Duration) error
}
