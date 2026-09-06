package portal

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/LeafYeeXYZ/BNUCourseGetter/internal/selection"
	"github.com/mxschmitt/playwright-go"
)

type cell struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type tableRow struct {
	Index   int      `json:"index"`
	Headers []string `json:"headers"`
	Cells   []cell   `json:"cells"`
}
type matchedRow struct {
	tableRow
	selection.Candidate
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }

func (r tableRow) field(suffixes []string, headers ...string) string {
	for i, c := range r.Cells {
		for _, suffix := range suffixes {
			if c.ID != "" && strings.HasSuffix(c.ID, "_"+suffix) {
				return strings.TrimSpace(c.Text)
			}
		}
		if i < len(r.Headers) {
			for _, h := range headers {
				if compact(r.Headers[i]) == h {
					return strings.TrimSpace(c.Text)
				}
			}
		}
	}
	return ""
}
func (r tableRow) course() string {
	return r.field([]string{"kcdm", "kch"}, "课程代码", "课程号", "课程编号")
}
func (r tableRow) class() string {
	return r.field([]string{"curent_skbjdm", "skbjdm", "skbh"}, "上课班号", "教学班号", "班号")
}
func (r tableRow) hasCourse(id string) bool {
	if code := r.course(); code != "" {
		return code == id
	}
	for _, c := range r.Cells {
		if strings.TrimSpace(c.Text) == id {
			return true
		}
	}
	return false
}

func (r tableRow) matchesSelectedCourse(target selection.Target) bool {
	code := r.course()
	return code == target.CourseID || code == target.CourseID+"-"+target.ClassID
}

func matchClass(rows []tableRow, target selection.Target, requireCode bool) (matchedRow, error) {
	var matches []tableRow
	for _, r := range rows {
		if r.class() == target.ClassID && (!requireCode || r.hasCourse(target.CourseID)) {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		return matchedRow{}, selection.Error("mismatch", "未找到唯一匹配的课程及班号，请核对课程信息和页面适配情况")
	}
	r := matches[0]
	n, err := strconv.Atoi(r.field([]string{"kxrs"}, "可选人数", "剩余人数", "剩余名额"))
	if err != nil || n < 0 {
		return matchedRow{}, selection.Error("mismatch", "无法识别目标班级的可选人数")
	}
	return matchedRow{tableRow: r, Candidate: selection.Candidate{
		Name:     r.field([]string{"kcmc"}, "课程名称", "课程"),
		Teacher:  r.field([]string{"rkjs", "jsmc"}, "任课教师", "教师", "教师姓名"),
		Schedule: r.field([]string{"sksj"}, "上课时间", "时间地点", "上课时间地点"), Remaining: n,
	}}, nil
}

// Only explicit term labels are evidence. The wall clock and unrelated course
// descriptions must never establish the semester of an enrollment.
var termPattern = regexp.MustCompile(`(?:学年学期|选课学期|当前学期)[：:\s]*([0-9]{4})\s*[-—～~]\s*([0-9]{4})[\s年学期/第：:（）()\-]*(秋|春|夏|一|二|三)`)

func termFromText(s string) string {
	m := termPattern.FindStringSubmatch(s)
	if len(m) == 0 {
		return ""
	}
	season := m[3]
	if season == "一" {
		season = "秋"
	}
	if season == "二" {
		season = "春"
	}
	if season == "三" {
		season = "夏"
	}
	return m[1] + "-" + m[2] + "/" + season
}

// Reads rendered table data, not scripts, credentials or browser storage.
func readRows(frame playwright.Frame) ([]tableRow, error) {
	value, err := frame.Evaluate(`() => Array.from(document.querySelectorAll('tr')).map((tr,index)=>{
	 const table=tr.closest('table');
	 const candidates=table?Array.from(table.rows):[];
	 const header=candidates.find(row=>Array.from(row.cells).some(c=>/^(课程代码|课程号|课程编号|上课班号|教学班号)$/.test(c.innerText.trim())));
	 return {index,headers:header?Array.from(header.cells,c=>c.innerText.trim()):[],
	 cells:tr.getClientRects().length?Array.from(tr.cells,c=>({id:c.id,text:c.innerText.trim()})):[]};
	}).filter(r=>r.cells.length)`)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var rows []tableRow
	err = json.Unmarshal(data, &rows)
	return rows, err
}

func readTerm(frame playwright.Frame) string {
	v, err := frame.Evaluate(`() => {
	 const text=document.body?.innerText || '';
	 const read=(name)=>{const e=document.querySelector('[name="'+name+'"], [name="electiveCourseForm.'+name+'"], #'+name);return e?.tagName==='SELECT'?e.selectedOptions[0]?.textContent:e?.value};
	 return {text,year:read('xn'),season:read('xq')};
	}`)
	if err != nil {
		return ""
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	text, _ := m["text"].(string)
	if term := termFromText(text); term != "" {
		return term
	}
	year, _ := m["year"].(string)
	season, _ := m["season"].(string)
	// Visible select labels can establish a term; unknown numeric codes cannot.
	return termFromText("学年学期：" + year + " " + season)
}
