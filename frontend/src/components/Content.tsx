import { Dialog, StartCourses, RehearseCourses, StopCourses } from '../wailsjs/go/main/App'
import { Form, Radio, Input, Button, Space, Select, Checkbox } from 'antd'
import { PlusOutlined, CloseOutlined } from '@ant-design/icons'
import { useZustand } from '../libs/useZustand'
import { useState, useRef, useEffect } from 'react'
import { EventsEmit, EventsOn } from '../wailsjs/runtime/runtime'
import { clearLegacySecrets, courseKey, loadCourses, normalizeCourses, saveSettings } from '../libs/courseSettings'
import type { Course, CourseRequest, CourseStatus } from '../libs/courseSettings'
import { CourseResults } from './CourseResults'

type FormValues = Omit<CourseRequest, 'headless' | 'useWebVpn'> & {
  network: 'webvpn' | 'intranet'
  _courseID: string
  _classID: string
  _type: 'public' | 'major'
}

clearLegacySecrets(localStorage)

export function Content() {
  const { browserStatus, systemStatus, currentStatus, importantStatus, disabled, setDisabled } = useZustand()
  const [form] = Form.useForm<FormValues>()
  const [stopping, setStopping] = useState(false)
  const [running, setRunning] = useState(false)
  const [courseStatuses, setCourseStatuses] = useState<Record<string, CourseStatus>>({})
  const activeRun = useRef<Promise<void> | null>(null)
  const busy = useRef(false)
  const stoppingRef = useRef(false)
  useEffect(() => EventsOn('courseStatus', (status: CourseStatus) => {
    setCourseStatuses(previous => ({ ...previous, [status.key]: status }))
  }), [])

  function reportError(error: unknown) {
    const message = `选课出错: ${String(error || '未知错误')}`
    EventsEmit('currentStatus', message)
    EventsEmit('importantStatus', message)
  }
  async function handleSubmit(value: FormValues, rehearsal = false) {
    if (busy.current || systemStatus !== '空闲') return
    if (browserStatus !== '已安装') {
      await Dialog('warning', '请等待浏览器安装完成；安装失败时请检查网络并重启应用')
      return
    }
    const normalized = normalizeCourses(courses)
    if (!normalized.length) { await Dialog('error', '请添加课程'); return }
    const request: CourseRequest = {
      mode: value.mode, speed: value.speed, studentID: value.studentID.trim(), password: value.password,
      courses: normalized, headless: rehearsal ? false : localStorage.getItem('isHeadless') !== 'no',
      useWebVpn: value.network === 'webvpn',
    }
    busy.current = true
    setDisabled(true)
    setRunning(true)
    setCourseStatuses(Object.fromEntries(normalized.map(course => [courseKey(course), { ...course, key: courseKey(course), state: 'pending', message: '等待查询' }])))
    try {
      // 演练显示浏览器，但不改变用户正式选课的显示偏好。
      saveSettings(localStorage, { ...request, headless: localStorage.getItem('isHeadless') !== 'no' })
      EventsEmit('systemStatus', rehearsal ? '演练中' : value.mode === 'CatchCourse' ? '抢课中' : '蹲课中')
      activeRun.current = rehearsal ? RehearseCourses(request) : StartCourses(request)
      await activeRun.current
    } catch (error) {
      reportError(error)
    } finally {
      activeRun.current = null
      if (!stoppingRef.current) finishRun()
    }
  }
  function finishRun() {
    busy.current = false
    setRunning(false)
    setDisabled(false)
    EventsEmit('systemStatus', '空闲')
  }
  async function handleStop() {
    if (!busy.current || stoppingRef.current) return
    stoppingRef.current = true
    setStopping(true)
    EventsEmit('systemStatus', '停止中')
    try {
      await StopCourses()
      await activeRun.current?.catch(() => undefined)
    } catch (error) {
      reportError(error)
      // 即使停止接口失败，也等待运行接口结束后才解锁表单。
      await activeRun.current?.catch(() => undefined)
    } finally {
      stoppingRef.current = false
      setStopping(false)
      finishRun()
    }
  }

  // 日志列表
  const logs = currentStatus.map((status, index) => (
    <p key={index} className='whitespace-nowrap overflow-x-auto opacity-85 text-xs'>{status}</p>
  ))
  const results = importantStatus.map((status, index) => (
    <p key={index} className='whitespace-nowrap overflow-x-auto opacity-85 text-xs'>{status}</p>
  ))
  // 自动滚动到底部
  const logsRef = useRef<HTMLDivElement>(null)
  const resultsRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    logsRef.current?.scrollTo(0, logsRef.current.scrollHeight)
  }, [logs])
  useEffect(() => {
    resultsRef.current?.scrollTo(0, resultsRef.current.scrollHeight)
  }, [results])

  // 课程列表
  const [courses, setCourses] = useState<Course[]>(() => loadCourses(localStorage))

  return (
    <div
      className='w-full h-full relative grid grid-rows-[1fr,10rem] overflow-hidden border-t border-rose-100 border-solid'
    >
      <div className='w-full flex flex-col items-center overflow-auto py-4'>
        <Form
          id='form'
          form={form}
          className='w-full max-w-lg py-4'
          disabled={disabled}
          autoComplete='off'
          layout='vertical'
          initialValues={{
            mode: localStorage.getItem('mode') || 'CatchCourse',
            speed: Number(localStorage.getItem('speed')) || 1000,
            _type: 'public',
            studentID: localStorage.getItem('studentID') || '',
            password: '',
            network: localStorage.getItem('network') || 'intranet',
          }}
          onFinish={async value => {
            await handleSubmit({ ...value, courses })
          }}
        >
          <Form.Item label='抢课模式' required style={{ marginBottom: '1rem' }}>
            <Space.Compact id='catch-mode' block>
              <Form.Item
                name='mode'
                noStyle
                rules={[{ required: true, message: '请选择抢课模式' }]}
              >
                <Radio.Group
                  id='catch-mode-select'
                  className='w-full'
                  block
                  options={[
                    { label: '抢课', value: 'CatchCourse' },
                    { label: '多线程蹲课', value: 'WatchCourse' },
                    { label: '单线程蹲课', value: 'WatchCourseSync' },
                  ]}
                  optionType='button'
                  buttonStyle='solid'
                />
              </Form.Item>
            </Space.Compact>
          </Form.Item>
          <Form.Item label='学号密码' required style={{ marginBottom: '1rem' }}>
            <Space.Compact block>
              <Form.Item
                name='studentID'
                noStyle
                rules={[{ required: true, message: '请输入学号' }]}
              >
                <Input id='student-id' style={{ width: '50%' }} placeholder='请输入学号' />
              </Form.Item>
              <Form.Item
                name='password'
                noStyle
                rules={[{ required: true, message: '请输入密码' }]}
              >
                <Input.Password id='student-password' style={{ width: '50%' }} placeholder='请输入密码' />
              </Form.Item>
            </Space.Compact>
          </Form.Item>
          <Form.Item label='其他设置' style={{ marginBottom: '1rem' }}>
            <Space.Compact block>
              <div className='text-nowrap bg-gray-100 border border-[#d9d9d9] border-e-0 rounded-s-md px-3 flex items-center justify-center'>
                刷新频率
              </div>
              <Form.Item
                noStyle
                name='speed'
                rules={[{ required: true, message: '请选择刷新频率' }]}
              >
                <Select
                  id='refresh-select'
                  options={[ // 刷新频率
                    { label: '每半秒', value: 500 },
                    { label: '每秒(推荐)', value: 1000 },
                    { label: '每两秒', value: 2000 },
                    { label: '每五秒', value: 5000 },
                  ]}
                />
              </Form.Item>
              <div className='text-nowrap bg-gray-100 border border-[#d9d9d9] border-e-0 px-3 flex items-center justify-center'>
                网络环境
              </div>
              <Form.Item
                noStyle
                name='network'
                rules={[{ required: true, message: '请选择网络环境' }]}
              >
                <Select
                  id='network-select'
                  options={[
                    { label: '校园网', value: 'intranet' },
                    { label: 'WebVPN', value: 'webvpn' },
                  ]}
                />
              </Form.Item>
              <div className='flex items-center justify-center border rounded-e-md border-[#d9d9d9] pl-3 pr-1'>
                <Checkbox
                  id='headless-select'
                  className='text-nowrap'
                  defaultChecked={localStorage.getItem('isHeadless') === 'no'}
                  onChange={e => {
                    if (e.target.checked) {
                      localStorage.setItem('isHeadless', 'no')
                    } else {
                      localStorage.setItem('isHeadless', 'yes')
                    }
                  }}
                >
                  显示浏览器
                </Checkbox>
              </div>
            </Space.Compact>
          </Form.Item>
          <Form.Item label='添加课程' style={{ marginBottom: '1rem' }}>
            <Space.Compact id='add-courses' block>
              <Form.Item noStyle name='_type'>
                <Select
                  id='course-type'
                  placeholder='课程类型'
                  options={[
                    { label: '选公共选修课', value: 'public' },
                    { label: '按开课计划选课', value: 'major' },
                  ]}
                />
              </Form.Item>
              <Form.Item noStyle name='_courseID'>
                <Input
                  id='course-id'
                  placeholder='课程代码, 例如 GE610088771' 
                  autoComplete='off' autoCorrect='off' autoCapitalize='off' spellCheck='false' 
                />
              </Form.Item>
              <Form.Item noStyle name='_classID'>
                <Input 
                  id='class-id'
                  placeholder='上课班号, 例如 01' 
                  autoComplete='off' autoCorrect='off' autoCapitalize='off' spellCheck='false' 
                />
              </Form.Item>
              <Button 
                id='add-course'
                type='primary' 
                className='border-gray-300 border-l-gray-200'
                icon={<PlusOutlined />} aria-label='添加课程'
                onClick={() => {
                  const courseID = form.getFieldValue('_courseID')?.trim()
                  const classID = form.getFieldValue('_classID')?.trim()
                  const type = form.getFieldValue('_type')
                  if (courseID && classID && type) {
                    setCourses(prev => normalizeCourses([...prev, { courseID, classID, type }]))
                    form.resetFields(['_courseID', '_classID', '_type'])
                  } else {
                    Dialog('error', '请输入课程类别、课程代码、上课班号')
                  }
                }} 
              />
            </Space.Compact>
          </Form.Item>

          <div 
            id='added-courses'
            className='mb-4 flex flex-wrap items-center justify-center text-nowrap gap-2'
          >
          {
            courses.length > 0 ? courses.map((course, index) => (
              <div key={courseKey(course)} className='flex items-center justify-center gap-2 border flex-nowrap text-xs py-1 px-2 rounded-full'>
                <p>{course.type === 'public' ? '选公共选修课' : '按开课计划选课'} | {course.courseID} | {course.classID}</p>
                <Button size='small' type='text' disabled={disabled} aria-label={`删除 ${course.courseID} 班号 ${course.classID}`} icon={<CloseOutlined />} onClick={() => {
                  setCourses(prev => prev.filter((_, i) => i !== index))
                }} />
              </div>
            )) : <p className='text-sm'>请添加课程</p>
          }
          </div>

          <Button
            type='default'
            htmlType='submit'
            block
            id='start-button'
          >
            开始选课
          </Button>
          <Button id='rehearse-button' block className='mt-2' onClick={async () => {
            try { const value = await form.validateFields(); await handleSubmit({ ...value, courses }, true) }
            catch { /* 表单字段显示验证错误。 */ }
          }}>只查询演练（显示浏览器）</Button>
          <p className='mt-2 text-xs'>演练只查询，不提交选课。密码仅在本次打开期间保存在内存。</p>
        </Form>
        <div className='w-full max-w-lg pb-4'>
          <Button id='stop-button' block danger disabled={!running || stopping} loading={stopping} onClick={handleStop}>
            {stopping ? '正在停止并清理…' : '停止任务'}
          </Button>
          <CourseResults statuses={Object.values(courseStatuses)} />
        </div>
      </div>

      <div className='w-full h-full grid grid-cols-2'>
        <section
          id='current-status'
          ref={logsRef}
          style={{ borderRight: '1px dashed #fda4af' }}
          className='p-2 border-y bg-[#fffaf9] border-rose-300 border-b-rose-100 border-solid overflow-auto'
        >
          {logs.length > 0 ? logs : <p className='w-full h-full flex items-center justify-center text-sm'>此处将显示日志</p>}
        </section>
        <section
          id='important-status'
          ref={resultsRef}
          className='p-2 border-y bg-[#fffaf9] border-rose-300 border-b-rose-100 border-solid overflow-auto'
        >
          {results.length > 0 ? results : <p className='w-full h-full flex items-center justify-center text-sm'>此处将显示结果</p>}
        </section>
      </div>

    </div>
  )
}