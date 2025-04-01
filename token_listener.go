package entraid

import (
	"github.com/redis-developer/go-redis-entraid/manager"
	"github.com/redis-developer/go-redis-entraid/token"
)

type entraidTokenListener struct {
	cp *entraidCredentialsProvider
}

func tokenListenerFromCP(cp *entraidCredentialsProvider) manager.TokenListener {
	return &entraidTokenListener{
		cp,
	}
}

func (l *entraidTokenListener) OnTokenNext(t *token.Token) {
	l.cp.onTokenNext(t)
}

func (l *entraidTokenListener) OnTokenError(err error) {
	l.cp.onTokenError(err)
}
