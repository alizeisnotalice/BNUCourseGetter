export type Course = { type: 'public' | 'major'; courseID: string; classID: string }
export type CourseRequest = {
  mode: 'CatchCourse' | 'WatchCourse' | 'WatchCourseSync'
  speed: number
  studentID: string
  password: string
  courses: Course[]
  headless: boolean
  useWebVpn: boolean
}
export type CourseStatus = Course & {
  key: string; state: string; message: string
  name?: string; teacher?: string; schedule?: string; term?: string; remaining?: number
}
type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export function courseKey(course: Course): string {
  return `${course.type}|${course.courseID}|${course.classID}`
}
export function normalizeCourses(input: unknown): Course[] {
  if (!Array.isArray(input)) return []
  const seen = new Set<string>()
  return input.flatMap((value) => {
    if (!value || !['public', 'major'].includes(value.type) || typeof value.courseID !== 'string' || typeof value.classID !== 'string') return []
    const course: Course = { type: value.type, courseID: value.courseID.trim(), classID: value.classID.trim() }
    const key = courseKey(course)
    if (!course.courseID || !course.classID || seen.has(key)) return []
    seen.add(key)
    return [course]
  })
}
export function clearLegacySecrets(storage: StorageLike): void {
  storage.removeItem('password')
  storage.removeItem('isRemember')
  storage.removeItem('isProtect')
}
export function loadCourses(storage: StorageLike): Course[] {
  try { return normalizeCourses(JSON.parse(storage.getItem('courses') ?? '[]')) } catch { return [] }
}
export function saveSettings(storage: StorageLike, value: CourseRequest): void {
  clearLegacySecrets(storage)
  storage.setItem('mode', value.mode)
  storage.setItem('speed', String(value.speed))
  storage.setItem('studentID', value.studentID.trim())
  storage.setItem('network', value.useWebVpn ? 'webvpn' : 'intranet')
  storage.setItem('isHeadless', value.headless ? 'yes' : 'no')
  storage.setItem('courses', JSON.stringify(normalizeCourses(value.courses)))
}
