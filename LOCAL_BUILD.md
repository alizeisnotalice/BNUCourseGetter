# 北师大选课助手：本地修改版

修改日期：2026-09-06。界面版本：2.3.1-safe.1。非学校官方软件。

本地版增加课程与班号唯一匹配、提交后结果核验、有限网络重试、停止任务及只查询演练；移除主界面的原作者品牌、推广链接及构建配置中的联系方式。保留来源和许可证，修复源码发布在个人 Fork 的 `fix/reliable-course-selection` 分支。

首次使用应连接校园网，或选择 WebVPN，再自行填写学号和密码，点击“只查询演练”。真实教务系统页面尚未通过本人登录后的兼容性验证；本地模拟测试不能替代该验证。

本机优先使用已有 Node.js 和 Chrome / Edge；若没有则需要在线下载运行组件。只查询演练不提交选课。结果未知时应在教务系统手动确认，避免再次提交。

已完成全量 Go 竞态测试、23 个本地浏览器集成场景、前端测试及类型检查、darwin/arm64 构建。浏览器集成测试使用独立 Edge 会话及本地模拟服务。

前端依赖审计仍有旧构建工具链相关告警，尚未完成升级；本次打包没有将它们描述为已修复。

## 来源与许可证

本地版基于 LeafYeeXYZ/BNUCourseGetter（d5a6b01360cbf1ecd2fac3cd6df65658a3e66a85），遵循原项目 GNU GPL v3 许可证。原项目及其贡献者的权利保留；本地修改版不代表原作者发布或维护。

完整许可证见 LICENSE。应用包的 Contents/Resources 中附有许可证、本说明与 corresponding-source.zip，包含本版本的源码及构建配置。原项目 README.md 保留为上游资料，其中旧界面和教程不代表本地版的当前行为。

## 构建

准备 Go 1.23.3、Bun 1.4.0 和 Wails 2.10.1，在源码目录运行：

```
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
GOTOOLCHAIN=go1.23.3 wails build -platform darwin/arm64
```

Mac 本地构建不包含 Apple Developer ID 公证。
