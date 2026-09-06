package main

// Legacy Wails entry points retain their signatures and share the new task runner.
func (a *App) CatchCoursePub(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("CatchCourse", "public", speed, studentID, password, courseID, classID, headless, useWebVPN)
}

func (a *App) CatchCourseMaj(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("CatchCourse", "major", speed, studentID, password, courseID, classID, headless, useWebVPN)
}
