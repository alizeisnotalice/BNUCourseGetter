import { describe, expect, test } from 'bun:test'
import { clearLegacySecrets, loadCourses, normalizeCourses, saveSettings } from '../src/libs/courseSettings'

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return { getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value) }, removeItem: (key: string) => { data.delete(key) } }
}
const courses = [{ type: 'major' as const, courseID: ' PHYS001 ', classID: ' 01 ' }]
describe('safe course settings', () => {
  test('startup removes legacy secrets but preserves course list and unrelated settings', () => {
    const store = memoryStorage({ password: 'secret', studentID: '2026123456', isRemember: 'yes', courses: JSON.stringify(courses), tutorial: 'done', speed: '2000' })
    clearLegacySecrets(store)
    expect(store.getItem('password')).toBeNull()
    expect(store.getItem('studentID')).toBeNull()
    expect(store.getItem('isRemember')).toBeNull()
    expect(store.getItem('courses')).toBe(JSON.stringify(courses))
    expect(store.getItem('tutorial')).toBe('done')
    expect(store.getItem('speed')).toBe('2000')
  })
  test('whitelists persisted settings and never writes password', () => {
    const store = memoryStorage({ password: 'old' })
    saveSettings(store, { mode: 'CatchCourse', speed: 1000, studentID: ' 123 ', password: 'new secret', courses, headless: false, useWebVpn: false })
    expect(store.getItem('password')).toBeNull()
    expect(store.getItem('studentID')).toBeNull()
    expect(loadCourses(store)).toEqual([{ type: 'major', courseID: 'PHYS001', classID: '01' }])
  })
  test('trims without losing leading zeros; deduplicates by category, code and class', () => {
    expect(normalizeCourses([...courses, { type: 'major', courseID: 'PHYS001', classID: '01' }, { type: 'public', courseID: 'PHYS001', classID: '01' }, { type: 'major', courseID: 'PHYS001', classID: '02' }, { type: 'major', courseID: ' ', classID: '01' }])).toEqual([
      { type: 'major', courseID: 'PHYS001', classID: '01' }, { type: 'public', courseID: 'PHYS001', classID: '01' }, { type: 'major', courseID: 'PHYS001', classID: '02' },
    ])
  })
  test('malformed saved courses do not crash startup', () => {
    expect(loadCourses(memoryStorage({ courses: '{bad' }))).toEqual([])
    expect(normalizeCourses([null, {}, { type: 'major', courseID: 123, classID: '01' }])).toEqual([])
  })
})
