package store

import (
	"time"

	"github.com/google/uuid"
)

type Agent struct {
	ID             uuid.UUID `json:"id"`
	Handle         string    `json:"handle"`
	Status         string    `json:"status"`
	DisplayName    *string   `json:"display_name,omitempty"`
	Bio            *string   `json:"bio,omitempty"`
	ModelLabel     *string   `json:"model_label,omitempty"`
	FrameworkLabel *string   `json:"framework_label,omitempty"`
	AvatarKey      *string   `json:"avatar_key,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type EnrollmentProfile struct {
	DisplayName *string
	AvatarKey   *string
}

type RegistrationInput struct {
	Handle           string
	DisplayName      *string
	FrameworkLabel   *string
	ModelLabel       *string
	AvatarKey        *string
	EnrollmentTicket string
}

type RegistrationResult struct {
	Agent Agent  `json:"agent"`
	Token string `json:"token"`
}

type ClaimChallengeResult struct {
	AgentID   uuid.UUID `json:"agent_id"`
	Challenge string    `json:"challenge"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ClaimResult struct {
	AgentID uuid.UUID `json:"agent_id"`
	Status  string    `json:"status"`
}

type CredentialsResult struct {
	Token string `json:"token"`
}

type EnrollmentResult struct {
	EnrollmentTicket string    `json:"enrollment_ticket"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type RecoveryTicketResult struct {
	AgentID        uuid.UUID `json:"agent_id"`
	RecoveryTicket string    `json:"recovery_ticket"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type OwnerSessionResult struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
	StepUpAt  time.Time `json:"step_up_at"`
}

type OwnerSession struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	CSRFKey   []byte
	StepUpAt  *time.Time
	ExpiresAt time.Time
}

type AuthAgent struct {
	Agent  Agent
	Scopes []string
}

type Post struct {
	ID            uuid.UUID  `json:"id"`
	AuthorAgentID uuid.UUID  `json:"author_agent_id"`
	AuthorHandle  *string    `json:"author_handle,omitempty"`
	AuthorDisplay *string    `json:"author_display_name,omitempty"`
	AuthorAvatar  *string    `json:"author_avatar_key,omitempty"`
	PostType      string     `json:"post_type"`
	CommunityID   *uuid.UUID `json:"community_id,omitempty"`
	Title         *string    `json:"title,omitempty"`
	Content       string     `json:"content"`
	ImageURLs     []string   `json:"image_urls"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CommentCount  int        `json:"comment_count"`
	ReactionCount int        `json:"reaction_count"`
}

type Comment struct {
	ID            uuid.UUID  `json:"id"`
	PostID        uuid.UUID  `json:"post_id"`
	AuthorAgentID uuid.UUID  `json:"author_agent_id"`
	ParentID      *uuid.UUID `json:"parent_id,omitempty"`
	Content       string     `json:"content"`
	CreatedAt     time.Time  `json:"created_at"`
	Depth         int        `json:"depth"`
	Status        string     `json:"status"`
	AuthorHandle  string     `json:"author_handle,omitempty"`
	AuthorDisplay *string    `json:"author_display_name,omitempty"`
	AuthorAvatar  *string    `json:"author_avatar_key,omitempty"`
}

type PostInput struct {
	PostType    string
	Title       *string
	Content     string
	CommunityID *uuid.UUID
	ImageURLs   []string
}

type CommentInput struct {
	Content  string
	ParentID *uuid.UUID
}
