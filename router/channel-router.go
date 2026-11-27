package router

import (
	"net/http"
	"path"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

type permissionRoute struct {
	method     string
	path       string
	permission authz.Permission
	handler    gin.HandlerFunc
}

// handlePermissionRoute registers a Casbin-guarded route and declares the same
// permission as the access token scope that route requires.
func handlePermissionRoute(group *gin.RouterGroup, method, relativePath string, permission authz.Permission, handlers ...gin.HandlerFunc) {
	middleware.DeclareAccessTokenPermissionRoute(method, joinPaths(group.BasePath(), relativePath), permission)
	group.Handle(method, relativePath, append([]gin.HandlerFunc{middleware.RequirePermission(permission)}, handlers...)...)
}

// joinPaths matches gin's route path joining, which keeps a trailing slash
// from the relative path, so declared keys equal c.FullPath().
func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if relativePath[len(relativePath)-1] == '/' && finalPath[len(finalPath)-1] != '/' {
		return finalPath + "/"
	}
	return finalPath
}

func registerChannelRoutes(apiRouter *gin.RouterGroup) {
	channelRoute := apiRouter.Group("/channel")
	channelRoute.Use(middleware.AdminAuth())

	channelRoute.POST("/:id/key",
		middleware.RootAuth(),
		middleware.CriticalRateLimit(),
		middleware.DisableCache(),
		middleware.SecureVerificationRequired(),
		controller.GetChannelKey,
	)

	for _, route := range channelPermissionRoutes {
		handlePermissionRoute(channelRoute, route.method, route.path, route.permission, route.handler)
	}
}

var channelPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/priority/order", permission: authz.ChannelRead, handler: controller.GetChannelPriorityOrder},
	{method: http.MethodPost, path: "/priority/reorder", permission: authz.ChannelWrite, handler: controller.ReorderChannelPriority},
	{method: http.MethodGet, path: "/", permission: authz.ChannelRead, handler: controller.GetAllChannels},
	{method: http.MethodGet, path: "/search", permission: authz.ChannelRead, handler: controller.SearchChannels},
	{method: http.MethodGet, path: "/models", permission: authz.ChannelRead, handler: controller.ChannelListModels},
	{method: http.MethodGet, path: "/default_base_urls", permission: authz.ChannelRead, handler: controller.GetChannelDefaultBaseURLs},
	{method: http.MethodGet, path: "/models_enabled", permission: authz.ChannelRead, handler: controller.EnabledListModels},
	{method: http.MethodGet, path: "/model-options", permission: authz.ChannelRead, handler: controller.GetChannelModelOptions},
	{method: http.MethodGet, path: "/ops", permission: authz.ChannelRead, handler: controller.GetChannelOps},
	{method: http.MethodGet, path: "/:id/user-concurrency", permission: authz.ChannelRead, handler: controller.GetChannelUserConcurrency},
	{method: http.MethodGet, path: "/:id/limit-parent", permission: authz.ChannelRead, handler: controller.GetChannelLimitParent},
	{method: http.MethodGet, path: "/:id/limit-parent/options", permission: authz.ChannelRead, handler: controller.GetChannelLimitParentOptions},
	{method: http.MethodPut, path: "/:id/limit-parent", permission: authz.ChannelOperate, handler: controller.SetChannelLimitParent},
	{method: http.MethodGet, path: "/:id/user-limit-users", permission: authz.ChannelRead, handler: controller.SearchChannelUserLimitUsers},
	{method: http.MethodGet, path: "/:id/user-limit-status/:user_id", permission: authz.ChannelRead, handler: controller.GetChannelUserLimitStatus},
	{method: http.MethodGet, path: "/:id/user-limit-overrides", permission: authz.ChannelRead, handler: controller.GetChannelUserLimitOverrides},
	{method: http.MethodPut, path: "/:id/user-limit-overrides/:user_id", permission: authz.ChannelOperate, handler: controller.SetChannelUserLimitOverride},
	{method: http.MethodDelete, path: "/:id/user-limit-overrides/:user_id", permission: authz.ChannelOperate, handler: controller.DeleteChannelUserLimitOverride},
	{method: http.MethodGet, path: "/:id/period-policy", permission: authz.ChannelRead, handler: controller.GetChannelPeriodPolicy},
	{method: http.MethodPut, path: "/:id/period-policy", permission: authz.ChannelOperate, handler: controller.SetChannelPeriodPolicy},
	{method: http.MethodPost, path: "/:id/period-policy/preview", permission: authz.ChannelRead, handler: controller.PreviewChannelPeriodPolicy},
	{method: http.MethodGet, path: "/:id/period-policy/targets", permission: authz.ChannelRead, handler: controller.GetChannelPeriodPolicyTargets},
	{method: http.MethodGet, path: "/:id/budgets/usage-summary", permission: authz.ChannelRead, handler: controller.GetChannelBudgetUsageSummary},
	{method: http.MethodGet, path: "/:id/budgets/:budget_id/usage", permission: authz.ChannelRead, handler: controller.GetChannelBudgetUsage},
	{method: http.MethodPut, path: "/:id/budgets/:budget_id/usage", permission: authz.ChannelOperate, handler: controller.SetChannelBudgetUsage},
	{method: http.MethodPut, path: "/:id/budgets/:budget_id/user-overrides/:user_id", permission: authz.ChannelOperate, handler: controller.SetChannelUserBudgetOverride},
	{method: http.MethodDelete, path: "/:id/budgets/:budget_id/user-overrides/:user_id", permission: authz.ChannelOperate, handler: controller.DeleteChannelUserBudgetOverride},
	{method: http.MethodGet, path: "/:id/budget-user-overrides", permission: authz.ChannelRead, handler: controller.GetChannelUserBudgetOverrides},
	{method: http.MethodGet, path: "/:id", permission: authz.ChannelRead, handler: controller.GetChannel},
	{method: http.MethodGet, path: "/test", permission: authz.ChannelOperate, handler: controller.TestAllChannels},
	{method: http.MethodGet, path: "/test/:id", permission: authz.ChannelOperate, handler: controller.TestChannel},
	{method: http.MethodGet, path: "/update_balance", permission: authz.ChannelOperate, handler: controller.UpdateAllChannelsBalance},
	{method: http.MethodGet, path: "/update_balance/:id", permission: authz.ChannelOperate, handler: controller.UpdateChannelBalance},
	{method: http.MethodGet, path: "/update_balance/task/:task_id", permission: authz.ChannelOperate, handler: controller.GetChannelBalanceTask},
	{method: http.MethodPost, path: "/", permission: authz.ChannelSensitiveWrite, handler: controller.AddChannel},
	{method: http.MethodPut, path: "/", permission: authz.ChannelWrite, handler: controller.UpdateChannel},
	{method: http.MethodPost, path: "/status/batch", permission: authz.ChannelOperate, handler: controller.BatchUpdateChannelStatus},
	{method: http.MethodPost, path: "/:id/status", permission: authz.ChannelOperate, handler: controller.UpdateChannelStatus},
	{method: http.MethodDelete, path: "/disabled", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteDisabledChannel},
	{method: http.MethodPost, path: "/tag/disabled", permission: authz.ChannelOperate, handler: controller.DisableTagChannels},
	{method: http.MethodPost, path: "/tag/enabled", permission: authz.ChannelOperate, handler: controller.EnableTagChannels},
	{method: http.MethodPut, path: "/tag", permission: authz.ChannelWrite, handler: controller.EditTagChannels},
	{method: http.MethodDelete, path: "/:id", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteChannel},
	{method: http.MethodPost, path: "/batch", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteChannelBatch},
	{method: http.MethodPost, path: "/fix", permission: authz.ChannelOperate, handler: controller.FixChannelsAbilities},
	{method: http.MethodGet, path: "/fetch_models/:id", permission: authz.ChannelOperate, handler: controller.FetchUpstreamModels},
	{method: http.MethodGet, path: "/:id/vllm/status", permission: authz.ChannelRead, handler: controller.GetVLLMChannelStatus},
	{method: http.MethodGet, path: "/:id/sglang/status", permission: authz.ChannelRead, handler: controller.GetSGLangChannelStatus},
	{method: http.MethodPost, path: "/fetch_models", permission: authz.ChannelSensitiveWrite, handler: controller.FetchModels},
	{method: http.MethodPost, path: "/:id/codex/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshCodexChannelCredential},
	{method: http.MethodPost, path: "/:id/plugin_login", permission: authz.ChannelSensitiveWrite, handler: controller.StartPluginLogin},
	{method: http.MethodPost, path: "/:id/plugin_login/continue", permission: authz.ChannelSensitiveWrite, handler: controller.ContinuePluginLogin},
	{method: http.MethodPost, path: "/plugin_login/callback", permission: authz.ChannelSensitiveWrite, handler: controller.CompletePluginLoginCallback},
	{method: http.MethodGet, path: "/:id/plugin_credentials", permission: authz.ChannelRead, handler: controller.GetPluginCredentials},
	{method: http.MethodGet, path: "/:id/codex/usage", permission: authz.ChannelRead, handler: controller.GetCodexChannelUsage},
	{method: http.MethodGet, path: "/:id/codex/usage/reset-credits", permission: authz.ChannelRead, handler: controller.GetCodexChannelRateLimitResetCredits},
	{method: http.MethodPost, path: "/:id/codex/usage/reset", permission: authz.ChannelOperate, handler: controller.ResetCodexChannelUsage},
	{method: http.MethodPost, path: "/ollama/pull", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaPullModel},
	{method: http.MethodPost, path: "/ollama/pull/stream", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaPullModelStream},
	{method: http.MethodDelete, path: "/ollama/delete", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaDeleteModel},
	{method: http.MethodGet, path: "/ollama/version/:id", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaVersion},
	{method: http.MethodPost, path: "/batch/tag", permission: authz.ChannelWrite, handler: controller.BatchSetChannelTag},
	{method: http.MethodGet, path: "/tag/models", permission: authz.ChannelRead, handler: controller.GetTagModels},
	{method: http.MethodPost, path: "/copy/:id", permission: authz.ChannelSensitiveWrite, handler: controller.CopyChannel},
	{method: http.MethodPost, path: "/multi_key/manage", permission: authz.ChannelOperate, handler: controller.ManageMultiKeys},
	{method: http.MethodPost, path: "/upstream_updates/apply", permission: authz.ChannelWrite, handler: controller.ApplyChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/apply_all", permission: authz.ChannelWrite, handler: controller.ApplyAllChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect", permission: authz.ChannelOperate, handler: controller.DetectChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect_all", permission: authz.ChannelOperate, handler: controller.DetectAllChannelUpstreamModelUpdates},
}
