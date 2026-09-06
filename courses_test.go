package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
)

func TestLegacyRejectsUnpairedCourseArrays(t *testing.T) {
	app := NewApp()
	if err := app.CatchCourseMaj(1000, "student", "test-only", []string{"PHY"}, nil, false, false); err == nil {
		t.Fatal("unpaired arrays accepted")
	}
}

func TestFirstRegularFileSkipsMissingPathsAndDirectories(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "browser")
	if err := os.WriteFile(file, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := firstRegularFile([]string{"", filepath.Join(dir, "missing"), dir, file}); got != file {
		t.Fatalf("got %q", got)
	}
}

func TestRequestValidationBeforeBrowserCreation(t *testing.T) {
	base := CourseRequest{Mode: "CatchCourse", Speed: 1000, StudentID: " student ", Password: " test-only ", Courses: []selection.Target{{Type: "major", CourseID: " PHY ", ClassID: " 01 "}}}
	r, err := validateRequest(base)
	if err != nil || r.StudentID != "student" || r.Password != " test-only " || r.Courses[0].ClassID != "01" {
		t.Fatalf("%+v %v", r.Courses, err)
	}
	for _, change := range []func(*CourseRequest){func(r *CourseRequest) { r.Speed = 0 }, func(r *CourseRequest) { r.Mode = "bad" }, func(r *CourseRequest) { r.Courses = nil }, func(r *CourseRequest) { r.Password = "" }} {
		req := base
		change(&req)
		if _, err := validateRequest(req); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	for _, req := range []CourseRequest{
		{Mode: "CatchCourse", Speed: 1000, StudentID: "bad\nstudent", Password: "test-only", Courses: base.Courses},
		{Mode: "CatchCourse", Speed: 1000, StudentID: "student", Password: strings.Repeat("x", 1025), Courses: base.Courses},
	} {
		if _, err := validateRequest(req); err == nil {
			t.Fatal("oversized or control-character credential accepted")
		}
	}
}

func TestStopWaitsForCleanupAndIsIdempotent(t *testing.T) {
	app := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	app.cancel, app.done = cancel, done
	stopped := make(chan struct{})
	go func() { _ = app.StopCourses(); close(stopped) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("did not cancel")
	}
	select {
	case <-stopped:
		t.Fatal("returned before cleanup")
	default:
	}
	app.mu.Lock()
	app.cancel = nil
	app.done = nil
	close(done)
	app.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("did not return after cleanup")
	}
	if err := app.StopCourses(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentStartCannotReplaceActiveTask(t *testing.T) {
	app := NewApp()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.cancel = cancel
	req := CourseRequest{Mode: "CatchCourse", Speed: 1000, StudentID: "test", Password: "test-only", Courses: []selection.Target{{Type: "major", CourseID: "PHY", ClassID: "01"}}}
	if err := app.StartCourses(req); err == nil {
		t.Fatal("accepted a second run")
	}
	if app.cancel == nil {
		t.Fatal("replaced active task")
	}
}
