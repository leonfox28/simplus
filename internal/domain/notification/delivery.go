package notification

import "time"

var EventKinds = []string{"sms.received", "sms.failed", "call.incoming", "call.missed", "system.degraded", "vowifi.connected", "vowifi.disconnected", "cellular.connected", "cellular.disconnected"}

type Event struct {
	Key, Kind, ObjectID, ConnectionKind string
	Message, FeishuMessage              string
	ObservedAt                          time.Time
}

type Delivery struct {
	ID                                                      int64
	ChannelID, EventKind, ObjectID, ConnectionKind, Message string
	Attempts                                                int64
}

type Counts struct{ Pending, Failed int64 }

type Observation struct {
	ObjectID, DisplayName, Kind, Reason string
	Connected                           bool
	ObservedAt                          time.Time
}

type Cursor struct {
	Source, Instance string
	Sequence         uint64
	Gaps             int64
}

// ConnectionMessage deliberately accepts only a fixed reason vocabulary.
func ConnectionMessage(o Observation) string {
	kind := "蜂窝网络"
	if o.Kind == "vowifi" {
		kind = "VoWiFi"
	}
	state := "已断开"
	if o.Connected {
		state = "已连接"
	}
	reason := map[string]string{"registered": "注册成功", "registration_lost": "失去网络注册", "device_removed": "模组已拔出", "rf_off": "射频已关闭", "sim_unavailable": "SIM 不可用", "stopped": "主动停用", "reconnecting": "正在重新连接", "worker_exited": "连接进程退出"}[o.Reason]
	message := "[Simplus] " + o.DisplayName + "\n" + kind + "：" + state + "\n观测时间：" + o.ObservedAt.UTC().Format(time.RFC3339)
	if reason != "" {
		message += "\n原因：" + reason
	}
	return message
}
