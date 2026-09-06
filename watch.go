package main

// Legacy Wails entry points retain their signatures and share the new task runner.
func (a *App) WatchCoursePub(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("WatchCourse", "public", speed, studentID, password, courseID, classID, headless, useWebVPN)
}

func (a *App) WatchCoursePubSync(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("WatchCourseSync", "public", speed, studentID, password, courseID, classID, headless, useWebVPN)
}

func (a *App) WatchCourseMaj(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("WatchCourse", "major", speed, studentID, password, courseID, classID, headless, useWebVPN)
}

func (a *App) WatchCourseMajSync(speed int, studentID, password string, courseID, classID []string, headless, useWebVPN bool) error {
	return a.legacyRun("WatchCourseSync", "major", speed, studentID, password, courseID, classID, headless, useWebVPN)
}
