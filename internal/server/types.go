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
	DurationDays int `json:"duration_days" binding:"required,min=1,max=3650"`
}

type patchManagerStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active expired disabled"`
}

type managerRedeemKeyRequest struct {
	Code string `json:"code" binding:"required,min=6,max=64"`
}

type createActivationCodeRequest struct {
	DurationDays int `json:"duration_days" binding:"required,min=1,max=3650"`
}

type quickCreateUserRequest struct {
	DurationDays int `json:"duration_days" binding:"required,min=1,max=3650"`
}

type putTaskConfigRequest struct {
	TaskConfig map[string]any `json:"task_config" binding:"required"`
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
	NodeID       string `json:"node_id" binding:"required,min=3,max=128"`
	Limit        int    `json:"limit"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type agentJobUpdateRequest struct {
	NodeID       string `json:"node_id" binding:"required,min=3,max=128"`
	Message      string `json:"message"`
	ErrorCode    string `json:"error_code"`
	LeaseSeconds int    `json:"lease_seconds"`
}
