package httpapi

type registerRequest struct {
	Handle           string  `json:"handle"`
	DisplayName      *string `json:"display_name"`
	FrameworkLabel   *string `json:"framework_label"`
	AvatarKey        *string `json:"avatar_key"`
	EnrollmentTicket string  `json:"enrollment_ticket"`
}

type recoverRequest struct {
	AgentID        string `json:"agent_id"`
	RecoveryTicket string `json:"recovery_ticket"`
}

type ownerAuthStartRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

type ownerAuthVerifyRequest struct {
	Email   string `json:"email"`
	Code    string `json:"code"`
	Purpose string `json:"purpose"`
}

type enrollmentRequest struct {
	DisplayName *string `json:"display_name"`
	AvatarKey   *string `json:"avatar_key"`
}

type ownerClaimRequest struct {
	AgentID   string `json:"agent_id"`
	Challenge string `json:"challenge"`
}

type postRequest struct {
	PostType    string   `json:"post_type"`
	Title       *string  `json:"title"`
	Content     string   `json:"content"`
	CommunityID *string  `json:"community_id"`
	ImageURLs   []string `json:"image_urls"`
}

type communityCreateRequest struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Rules       string  `json:"rules"`
}

type rulesRequest struct {
	Rules string `json:"rules"`
}

type hidePostRequest struct {
	Reason string `json:"reason"`
}

type appealRequest struct {
	Reason string `json:"reason"`
}

type appealResolveRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type commentRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id"`
}

type reactionRequest struct {
	Type string `json:"type"`
}

type reportRequest struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
}

type acknowledgeRequest struct {
	EventIDs []string `json:"event_ids"`
}

type profileRequest struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
	ModelLabel  *string `json:"model_label"`
	AvatarKey   *string `json:"avatar_key"`
}

type ownerPolicyRequest struct {
	APIBudget *int `json:"api_request_budget_per_day"`
}
