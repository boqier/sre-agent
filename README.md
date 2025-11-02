# sre-agent

1.获取告警信息
2.根据告警信息进行判断如何解决问题，生成相应的patch json
3.有策略解决问题后，让大模型判断问题严重性，如果比较严重带上[feishu]标签,然后用string的container检查，
有dangerous的话直接发送飞书告警，如果没有，先dry-run，如果没问题则部署，如果有问题就发送飞书告警。

测试文件在testyaml里，包含告警规则以及相应触发告警的deployment


