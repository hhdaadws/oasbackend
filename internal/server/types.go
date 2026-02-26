package server

type bootstrapInitRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

type createRenewalKeyRequest struct {
	DurationDays int    `json:"duration_days" binding:"required,min=1,max=3650"`
	ManagerType  string `json:"manager_type" binding:"required,oneof=daily duiyi shuaka all"`
}

type patchCodeStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=revoked"`
}

type managerRedeemKeyRequest struct {
	Code string `json:"code" binding:"required,min=6,max=64"`
}

type managerPutAliasRequest struct {
	Alias string `json:"alias" binding:"max=64"`
}

type createActivationCodeRequest struct {
	DurationDays int    `json:"duration_days" binding:"required,min=1,max=3650"`
	UserType     string `json:"user_type" binding:"required,oneof=daily duiyi shuaka foster jingzhi"`
}

type quickCreateUserRequest struct {
	DurationDays int    `json:"duration_days" binding:"required,min=1,max=3650"`
	UserType     string `json:"user_type" binding:"required,oneof=daily duiyi shuaka foster jingzhi"`
}

type putTaskConfigRequest struct {
	TaskConfig map[string]any `json:"task_config" binding:"required"`
}

type managerPatchUserLifecycleRequest struct {
	ExpiresAt     string `json:"expires_at"`
	ExtendDays    int    `json:"extend_days"`
	Status        string `json:"status"`
	ArchiveStatus string `json:"archive_status"`
}

type managerPatchUserSettingsRequest struct {
	CanViewLogs *bool `json:"can_view_logs"`
}

type superPatchManagerLifecycleRequest struct {
	ExpiresAt   string `json:"expires_at"`
	ExtendDays  int    `json:"extend_days"`
	ManagerType string `json:"manager_type" binding:"omitempty,oneof=daily duiyi shuaka all"`
}

type superResetManagerPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required,min=6,max=128"`
}

type putUserAssetsRequest struct {
	Assets map[string]any `json:"assets" binding:"required"`
}

type userRegisterByCodeRequest struct {
	Code string `json:"code" binding:"required,min=6,max=64"`
}

type userLoginRequest struct {
	AccountNo  string `json:"account_no" binding:"required,min=6,max=64"`
	DeviceInfo string `json:"device_info"`
}

type userRedeemCodeRequest struct {
	Code string `json:"code" binding:"required,min=6,max=64"`
}

type agentLoginRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
	NodeID   string `json:"node_id" binding:"required,min=3,max=128"`
	Version  string `json:"version"`
}

type agentPollJobsRequest struct {
	NodeID       string   `json:"node_id" binding:"required,min=3,max=128"`
	Limit        int      `json:"limit"`
	LeaseSeconds int      `json:"lease_seconds"`
	UserTypes    []string `json:"user_types"` // 可选，按用户类型过滤
}

type agentJobUpdateRequest struct {
	NodeID       string         `json:"node_id" binding:"required,min=3,max=128"`
	Message      string         `json:"message"`
	ErrorCode    string         `json:"error_code"`
	LeaseSeconds int            `json:"lease_seconds"`
	Result       map[string]any `json:"result"`
}

// ── Batch request types ───────────────────────────────

type batchUserLifecycleRequest struct {
	UserIDs    []uint `json:"user_ids" binding:"required,min=1,max=500"`
	ExpiresAt  string `json:"expires_at"`
	ExtendDays int    `json:"extend_days"`
	Status     string `json:"status"`
}

type batchUserAssetsRequest struct {
	UserIDs []uint         `json:"user_ids" binding:"required,min=1,max=500"`
	Assets  map[string]any `json:"assets" binding:"required"`
}

type batchCodeRevokeRequest struct {
	CodeIDs []uint `json:"code_ids" binding:"required,min=1,max=500"`
}

type batchManagerLifecycleRequest struct {
	ManagerIDs []uint `json:"manager_ids" binding:"required,min=1,max=200"`
	ExpiresAt  string `json:"expires_at"`
	ExtendDays int    `json:"extend_days"`
}

type batchRenewalKeyRevokeRequest struct {
	KeyIDs []uint `json:"key_ids" binding:"required,min=1,max=500"`
}

type batchRenewalKeyDeleteRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1,max=500"`
}

type batchActivationCodeDeleteRequest struct {
	CodeIDs []uint `json:"code_ids" binding:"required,min=1,max=500"`
}

type batchUserDeleteRequest struct {
	UserIDs []uint `json:"user_ids" binding:"required,min=1,max=500"`
}

// ── Scan request types ───────────────────────────────

type userScanCreateRequest struct {
	LoginID string `json:"login_id" binding:"max=64"`
}

type userScanChoiceRequest struct {
	ScanJobID  uint   `json:"scan_job_id" binding:"required"`
	ChoiceType string `json:"choice_type" binding:"required,oneof=system zone role"`
	Value      string `json:"value" binding:"required"`
}

type userScanCancelRequest struct {
	ScanJobID uint `json:"scan_job_id" binding:"required"`
}

type userScanHeartbeatRequest struct {
	ScanJobID uint `json:"scan_job_id" binding:"required"`
}

type agentScanPollRequest struct {
	NodeID       string `json:"node_id" binding:"required,min=3,max=128"`
	Limit        int    `json:"limit"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type agentScanPhaseRequest struct {
	NodeID        string `json:"node_id" binding:"required,min=3,max=128"`
	Phase         string `json:"phase" binding:"required"`
	Screenshot    string `json:"screenshot"`
	ScreenshotKey string `json:"screenshot_key"`
}

type agentScanStartRequest struct {
	NodeID       string `json:"node_id" binding:"required,min=3,max=128"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type agentScanHeartbeatRequest struct {
	NodeID       string `json:"node_id" binding:"required,min=3,max=128"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type agentScanCompleteRequest struct {
	NodeID  string `json:"node_id" binding:"required,min=3,max=128"`
	Message string `json:"message"`
}

type agentScanFailRequest struct {
	NodeID    string `json:"node_id" binding:"required,min=3,max=128"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
}

type putDuiyiAnswersRequest struct {
	Answers map[string]*string `json:"answers" binding:"required"`
}

// Single-window answer request (used after time-window restriction)
type putSingleWindowAnswerRequest struct {
	Window string `json:"window" binding:"required"`
	Answer string `json:"answer" binding:"required"`
}

type createBloggerRequest struct {
	Name string `json:"name" binding:"required,min=1,max=64"`
}

type putDuiyiAnswerSourceRequest struct {
	Source    string `json:"source" binding:"required,oneof=manager blogger"`
	BloggerID *uint `json:"blogger_id"`
}

// ── Friend request types ───────────────────────────────

type userFriendRequest struct {
	FriendUsername string `json:"friend_username" binding:"required,min=1,max=128"`
}

// ── Team Yuhun request types ───────────────────────────

type userTeamYuhunCreateRequest struct {
	FriendID    uint           `json:"friend_id" binding:"required"`
	ScheduledAt string         `json:"scheduled_at" binding:"required"`
	Role        string         `json:"role" binding:"required,oneof=driver attacker"`
	Lineup      map[string]any `json:"lineup"`
}

type userTeamYuhunAcceptRequest struct {
	Role   string         `json:"role" binding:"required,oneof=driver attacker"`
	Lineup map[string]any `json:"lineup"`
}
