package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestClientProfileEnforcementPluginExecutor(t *testing.T) {
	for _, kind := range []string{"execute", "count", "stream"} {
		t.Run(kind, func(t *testing.T) {
			host := &mockPluginUsageHost{}
			host.hasRouters = true
			host.route = func(context.Context, pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, bool) {
				return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetExecutor, Target: "synthetic-executor"}, true
			}
			handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
			handler.SetModelRouterHost(host)
			ctx := clientprofiles.WithBinding(context.Background(), clientprofiles.Snapshot{Bound: true, Policies: map[string]clientprofiles.Policy{"claude": {Mode: "only", AccountRef: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"}, "codex": {Mode: "automatic"}}})
			var errMsg *interfaces.ErrorMessage
			switch kind {
			case "count":
				_, _, errMsg = handler.ExecuteCountWithAuthManager(ctx, "claude", "synthetic", []byte(`{"model":"synthetic"}`), "")
			case "stream":
				_, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "claude", "synthetic", []byte(`{"model":"synthetic"}`), "")
				errMsg = <-errChan
			default:
				_, _, errMsg = handler.ExecuteWithAuthManager(ctx, "claude", "synthetic", []byte(`{"model":"synthetic"}`), "")
			}
			if errMsg == nil || errMsg.StatusCode != 503 || !strings.Contains(errMsg.Error.Error(), "profile_plugin_unsupported") {
				t.Fatal(errMsg)
			}
			if host.lastPluginID != "" {
				t.Fatal("plugin executor was invoked", host.lastPluginID)
			}
		})
	}
}
