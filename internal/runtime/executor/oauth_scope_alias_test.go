package executor

import (
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestScopedClaudeExecutorsShareLazyAliasState(t *testing.T) {
	cfg, err := config.ParseConfigBytes([]byte("oauth: {providers: {codex: {header-defaults: {user-agent: oauth-agent}}}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"claude", "kimi"} {
		t.Run(provider, func(t *testing.T) {
			original := NewClaudeExecutor(cfg)
			scope := func() *ClaudeExecutor { return original.ForAPIKey().(*ClaudeExecutor) }
			if provider == "kimi" {
				kimi := NewKimiExecutor(cfg)
				original = kimi.ClaudeExecutor
				scope = func() *ClaudeExecutor { return kimi.ForAPIKey().(*KimiExecutor).ClaudeExecutor }
			}
			first, second := scope(), scope()
			first.claudeOAuthToolAliasStore().save([]string{"message:one"}, map[string]string{"original": "alias"})
			if aliases, ok := second.claudeOAuthToolAliasStore().load([]string{"message:one"}); !ok || aliases["original"] != "alias" {
				t.Fatal("separate scoped executors lost lazy shared alias state")
			}
			if first.cfg.CodexHeaderDefaults.UserAgent != "" || second.cfg.CodexHeaderDefaults.UserAgent != "" || original.cfg.CodexHeaderDefaults.UserAgent != "oauth-agent" {
				t.Fatal("API-key scoping changed original config or retained OAuth defaults")
			}
			var workers sync.WaitGroup
			for range 12 {
				workers.Go(func() {
					scoped := scope()
					scoped.claudeOAuthToolAliasStore().save([]string{"message:shared"}, map[string]string{"tool": "alias"})
				})
			}
			workers.Wait()
			if _, ok := original.claudeOAuthToolAliasStore().load([]string{"message:shared"}); !ok {
				t.Fatal("scoped state did not reach original executor")
			}
		})
	}
}
