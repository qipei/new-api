package model

import "sync"

var (
	consumeLogCreatedHooksMu sync.RWMutex
	consumeLogCreatedHooks   []func(int)
)

// RegisterConsumeLogCreatedHook registers lightweight post-create observers.
// Hooks must return quickly; long-running work should be queued asynchronously.
func RegisterConsumeLogCreatedHook(hook func(int)) {
	if hook == nil {
		return
	}
	consumeLogCreatedHooksMu.Lock()
	consumeLogCreatedHooks = append(consumeLogCreatedHooks, hook)
	consumeLogCreatedHooksMu.Unlock()
}

func notifyConsumeLogCreated(logID int) {
	if logID <= 0 {
		return
	}
	consumeLogCreatedHooksMu.RLock()
	hooks := append([]func(int){}, consumeLogCreatedHooks...)
	consumeLogCreatedHooksMu.RUnlock()
	for _, hook := range hooks {
		hook(logID)
	}
}
