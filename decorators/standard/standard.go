// Package standard applies the decorators every Topic should carry.
package standard

import (
	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/decorators/logging"
)

// Decorate wraps t in the standard decorators. Today that is logging.Topic.
func Decorate(t msg.Topic) msg.Topic {
	return logging.Topic(t)
}
