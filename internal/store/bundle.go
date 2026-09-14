package store

import (
	"context"

	"tdlibgo/internal/domain"
)

// AdminCommandStore coordinates administrative command logging and idempotency.
type AdminCommandStore interface {
	BeginCommand(ctx context.Context, cmd domain.AdminCommand) (domain.AdminCommand, bool, error)
	FinishCommand(ctx context.Context, commandID string, status domain.AdminCommandStatus, resultJSON []byte, errorText string) (domain.AdminCommand, error)
}

// AdminRestrictionStore manages account freeze and moderation restrictions.
type AdminRestrictionStore interface {
	GetAccountFreeze(ctx context.Context, userID int64) (domain.AccountFreeze, bool, error)
	SetAccountFreeze(ctx context.Context, freeze domain.AccountFreeze) (domain.AccountFreeze, error)
}

// Bundle brings all unified storage interfaces together into a single cohesive unit,
// abstracting whether the backend is pure in-memory (Zero-Docker mode) or PostgreSQL+Redis.
type Bundle struct {
	IsMemory                 bool
	AuthKeyStore             AuthKeyStore
	UserStore                UserStore
	AuthzStore               AuthorizationStore
	AdminCommands            AdminCommandStore
	AdminRestrictions        AdminRestrictionStore
	UpdateStateStore         UpdateStateStore
	UpdateEventStore         UpdateEventStore
	PhoneChangeStore         PhoneChangeStore
	CollectiblePhoneStore    CollectiblePhoneStore
	ReadModelVersionStore    ReadModelVersionStore
	DispatchOutboxStore      DispatchOutboxStore
	BootstrapUpdateStore     BootstrapUpdateJobStore
	BotAPIUpdateStore        BotAPIUpdateStore
	BotCallbackStore         BotCallbackRegistryStore
	EphemeralStore           EphemeralMessageStore
	EphemeralReportStore     EphemeralReportStore
	WelcomeMessageStore      WelcomeMessageStore
	ModerationReportStore    ModerationReportStore
	AuthDeliveryReportStore  AuthDeliveryReportStore
	ClientTelemetryStore     ClientTelemetryStore
	ContactStore             ContactStore
	DialogStore              DialogStore
	ChatlistStore            ChatlistStore
	MessageStore             MessageStore
	BroadcastStore           BroadcastStore
	ChannelStore             ChannelStore
	GroupCallStore           GroupCallStore
	HelpStore                HelpStore
	LangPackStore            LangPackStore
	BotStore                 BotStore
	UserCache                UserCache
	TempAuthKeyStore         TempAuthKeyBindingStore
	InlineRegistryStore      InlineRegistryStore
	CodeStore                CodeStore
	RateLimiter              RateLimiter
	PasswordStore            PasswordStore
	SecretChatStore          SecretChatStore
	StoryStore               StoryStore
	StarGiftStore            StarGiftStore
	StarsStore               StarsStore
	ThemesStore              ThemeStore
	VerificationStore        VerificationStore
	BotVerificationStore     BotVerificationStore
	PasskeyStore             PasskeyStore
	TelegramLoginStore       TelegramLoginStore
	PollStore                PollStore
	PrivacyStore             PrivacyStore
	AccountRatingStore       AccountRatingStore
	AIStore                  AIComposeStore
	AccountLifecycleStore    AccountLifecycleStore
}
