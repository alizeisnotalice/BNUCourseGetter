package portal

import (
	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"testing"
)

func TestUniqueCourseAndClassMatch(t *testing.T) {
	target := selection.Target{Type: "public", CourseID: "PHY001", ClassID: "01"}
	rows := []tableRow{
		{Index: 1, Headers: []string{"课程代码", "上课班号", "可选人数"}, Cells: []cell{{Text: " PHY001 "}, {Text: " 01 "}, {Text: " 3 "}}},
		{Index: 2, Headers: []string{"课程代码", "上课班号", "可选人数"}, Cells: []cell{{Text: "PHY0010"}, {Text: "01"}, {Text: "10"}}},
	}
	r, err := matchClass(rows, target, true)
	if err != nil || r.Index != 1 || r.Remaining != 3 {
		t.Fatalf("got %+v %v", r, err)
	}
	rows = append(rows, rows[0])
	if _, err = matchClass(rows, target, true); selection.Code(err) != "mismatch" {
		t.Fatalf("ambiguous: %v", err)
	}
}

func TestClassLeadingZeroAndMissingEvidence(t *testing.T) {
	target := selection.Target{Type: "major", CourseID: "PHY001", ClassID: "01"}
	row := tableRow{Cells: []cell{{ID: "tr0_curent_skbjdm", Text: "1"}, {ID: "tr0_kxrs", Text: "5"}}}
	if _, err := matchClass([]tableRow{row}, target, false); err == nil {
		t.Fatal("01 must not match 1")
	}
	row.Cells[0].Text = "01"
	if _, err := matchClass([]tableRow{row}, target, true); err == nil {
		t.Fatal("missing course code accepted")
	}
	row.Cells[1].Text = "未知"
	if _, err := matchClass([]tableRow{row}, target, false); err == nil {
		t.Fatal("unknown capacity accepted")
	}
}

func TestTargetTermMustBeExplicit(t *testing.T) {
	for _, s := range []string{"今天2026-09-05", "2026-2027 秋季学期的课程说明", ""} {
		if termFromText(s) != "" {
			t.Errorf("inferred term from %q", s)
		}
	}
	if got := termFromText("学年学期：2026-2027\n秋季学期\n时间区段：..."); got != "2026-2027/秋" {
		t.Fatal(got)
	}
	if got := termFromText("学年学期：2025-2026 春季学期"); got != "2025-2026/春" {
		t.Fatal(got)
	}
}

func TestSelectedCourseAllowsOnlyDocumentedClassSuffix(t *testing.T) {
	target := selection.Target{Type: "major", CourseID: "0410036171", ClassID: "02"}
	for _, code := range []string{"0410036171", "0410036171-02"} {
		row := tableRow{Headers: []string{"课程代码", "上课班号"}, Cells: []cell{{Text: code}, {Text: "02"}}}
		if !row.matchesSelectedCourse(target) {
			t.Fatalf("valid result code %q rejected", code)
		}
	}
	for _, code := range []string{"0410036171-01", "[0410036171]力学", "X0410036171-02"} {
		row := tableRow{Headers: []string{"课程代码", "上课班号"}, Cells: []cell{{Text: code}, {Text: "02"}}}
		if row.matchesSelectedCourse(target) {
			t.Fatalf("ambiguous result code %q accepted", code)
		}
	}
}
