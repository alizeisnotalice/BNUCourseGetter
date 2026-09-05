import type { CourseStatus } from '../libs/courseSettings'

const labels: Record<string, string> = {
  pending: '待查询', querying: '查询中', waiting: '等待开放或名额', ready: '演练查询完成',
  submitted: '已提交，待核验', selected: '已选上', full: '满员', conflict: '时间冲突',
  ineligible: '资格限制', auth_required: '需要登录处理', mismatch: '页面或课程不匹配',
  unknown: '结果未知，已暂停', failed: '失败', cancelled: '已停止',
}
export function CourseResults({ statuses }: { statuses: CourseStatus[] }) {
  if (!statuses.length) return null
  return <section aria-label='每门课程状态' className='mt-4'>
    <h2 className='text-sm font-semibold mb-2'>课程结果</h2>
    <ul className='space-y-2'>
      {statuses.map(status => <li key={status.key} className='border rounded p-2 text-xs break-words'>
        <p>{status.type === 'public' ? '公共选修课' : '按开课计划'} · {status.courseID} · 班号 {status.classID}</p>
        <p className='font-semibold mt-1'>{labels[status.state] ?? status.state}</p>
        <p>{status.message}</p>
        {status.name && <p>课程：{status.name}</p>}
        {status.teacher && <p>教师：{status.teacher}</p>}
        {status.schedule && <p>时间：{status.schedule}</p>}
        {status.term && <p>学期：{status.term}</p>}
        {status.remaining !== undefined && <p>剩余名额：{status.remaining}</p>}
      </li>)}
    </ul>
  </section>
}
