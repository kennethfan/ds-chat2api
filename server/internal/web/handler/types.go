package handler

import "ds-chat2api/server/internal/transport"

type CommandSender interface {
	SendRequest(method string, params interface{}) (transport.Message, error)
}

type StatusProvider interface {
	Connected() bool
	Stats() map[string]interface{}
}
