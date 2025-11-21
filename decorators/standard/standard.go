package standard

import (
	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/decorators/logging"
)

func Decorate(t msg.Topic) msg.Topic {
	return logging.Topic(t)
}
