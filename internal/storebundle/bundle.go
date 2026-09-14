package storebundle

import (
	"context"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"tdlibgo/internal/config"
	"tdlibgo/internal/store"
	"tdlibgo/internal/store/memory"
	"tdlibgo/internal/store/postgres"
	"tdlibgo/internal/store/redisstore"
)

// New initializes the appropriate storage bundle based on configuration.
// If storage driver is "memory" (or no Postgres DSN is set in auto mode),
// it initializes the full zero-dependency in-memory store (Zero-Docker mode).
func New(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*store.Bundle, func(), error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.StorageDriver))
	if driver == "" || driver == "auto" {
		// Auto-detect: if explicit DSN is provided and non-default, try postgres, otherwise memory
		hasExplicitPG := os.Getenv("TELESRV_POSTGRES_DSN") != "" || os.Getenv("POSTGRES_DSN") != ""
		if hasExplicitPG && cfg.PostgresDSN != "" {
			driver = "postgres"
		} else {
			driver = "memory"
		}
	}

	if driver == "memory" {
		logger.Info("Zero-Docker Mode enabled: Starting in-memory storage engine")
		bundle := newMemoryBundle(cfg, logger)
		return bundle, func() {}, nil
	}

	logger.Info("PostgreSQL & Redis Mode: Connecting to external databases",
		zap.String("postgres_dsn", maskDSN(cfg.PostgresDSN)),
		zap.String("redis_addr", cfg.RedisAddr))

	// Run PostgreSQL migrations
	migrationStatus, err := postgres.MigrateAndStatus(cfg.PostgresDSN)
	if err != nil {
		logger.Warn("PostgreSQL migration failed, falling back to Zero-Docker in-memory mode", zap.Error(err))
		bundle := newMemoryBundle(cfg, logger)
		return bundle, func() {}, nil
	}
	logger.Info("PostgreSQL schema ready",
		zap.Uint("schema_version", migrationStatus.Version),
		zap.Bool("schema_dirty", migrationStatus.Dirty))

	pool, err := postgres.Open(ctx, cfg.PostgresDSN,
		postgres.WithMaxConns(cfg.PostgresMaxConns),
		postgres.WithMinConns(cfg.PostgresMinConns),
	)
	if err != nil {
		logger.Warn("PostgreSQL connection failed, falling back to Zero-Docker in-memory mode", zap.Error(err))
		bundle := newMemoryBundle(cfg, logger)
		return bundle, func() {}, nil
	}

	rdb, err := redisstore.Open(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		logger.Warn("Redis connection failed, falling back to Zero-Docker in-memory mode", zap.Error(err))
		pool.Close()
		bundle := newMemoryBundle(cfg, logger)
		return bundle, func() {}, nil
	}

	cleanup := func() {
		_ = rdb.Close()
		pool.Close()
	}

	bundle := newPostgresBundle(pool, rdb, cfg, logger)
	return bundle, cleanup, nil
}

func newMemoryBundle(cfg *config.Config, logger *zap.Logger) *store.Bundle {
	userStore := memory.NewUserStore()
	usernameStore := memory.NewCollectibleUsernameStore()
	userStore.AttachUsernameRegistry(usernameStore)

	authKeyStore := memory.NewAuthKeyStore()
	authzStore := memory.NewAuthorizationStore()
	tempAuthKeyStore := memory.NewTempAuthKeyBindingStore(authKeyStore)
	adminStore := memory.NewAdminStore()

	events := memory.NewUpdateEventStore()
	updateStateStore := memory.NewUpdateStateStore()
	phoneChangeStore := memory.NewPhoneChangeStore(userStore, events)
	collectiblePhoneStore := memory.NewCollectiblePhoneStore()

	readModelStore := memory.NewReadModelVersionStore()
	dispatchOutboxStore := memory.NewDispatchOutboxStore()
	bootstrapUpdateStore := memory.NewBootstrapUpdateJobStore()
	botAPIUpdateStore := memory.NewBotAPIUpdateStore()
	botCallbackStore := memory.NewBotCallbackRegistryStore()

	ephemeralStore := memory.NewEphemeralMessageStore()
	ephemeralReportStore := memory.NewEphemeralReportStore()
	welcomeMessageStore := memory.NewWelcomeMessageStore()
	moderationReportStore := memory.NewModerationReportStore()
	authDeliveryReportStore := memory.NewAuthDeliveryReportStore()
	clientTelemetryStore := memory.NewClientTelemetryStore()

	contactStore := memory.NewContactStore()
	dialogStore := memory.NewDialogStore()
	chatlistStore := memory.NewChatlistStore()

	messageStore := memory.NewMessageStore(dialogStore)
	messageStore.AttachUpdateEventStore(events)

	broadcastStore := memory.NewBroadcastStore()
	channelStore := memory.NewChannelStore()
	groupCallStore := memory.NewGroupCallStore()
	helpStore := memory.NewHelpStore()
	langPackStore := memory.NewLangPackStore()

	botStore := memory.NewBotStore(userStore)
	userCache := memory.NewUserCache()
	inlineRegistryStore := memory.NewInlineRegistryStore()
	codeStore := memory.NewCodeStore()
	rateLimiter := memory.NewRateLimiter()
	passwordStore := memory.NewPasswordStore()
	secretChatStore := memory.NewSecretChatStore()
	storyStore := memory.NewStoryStore(channelStore)
	starGiftStore := memory.NewStarGiftStore()
	starsStore := memory.NewStarsStore()
	themesStore := memory.NewThemeStore()
	verificationStore := memory.NewVerificationStore()
	botVerificationStore := memory.NewBotVerificationStore()
	passkeyStore := memory.NewPasskeyStore()
	telegramLoginStore := memory.NewTelegramLoginStore(botStore)
	pollStore := memory.NewPollStore()
	privacyStore := memory.NewPrivacyStore()
	accountRatingStore := memory.NewAccountRatingStore()
	aiStore := memory.NewAIComposeStore()
	accountLifecycleStore := memory.NewAccountLifecycleStore(userStore, passwordStore)

	return &store.Bundle{
		IsMemory:                true,
		AuthKeyStore:            authKeyStore,
		UserStore:               userStore,
		AuthzStore:              authzStore,
		AdminCommands:           adminStore,
		AdminRestrictions:       adminStore,
		UpdateStateStore:        updateStateStore,
		UpdateEventStore:        events,
		PhoneChangeStore:        phoneChangeStore,
		CollectiblePhoneStore:   collectiblePhoneStore,
		ReadModelVersionStore:   readModelStore,
		DispatchOutboxStore:     dispatchOutboxStore,
		BootstrapUpdateStore:    bootstrapUpdateStore,
		BotAPIUpdateStore:       botAPIUpdateStore,
		BotCallbackStore:        botCallbackStore,
		EphemeralStore:          ephemeralStore,
		EphemeralReportStore:    ephemeralReportStore,
		WelcomeMessageStore:     welcomeMessageStore,
		ModerationReportStore:   moderationReportStore,
		AuthDeliveryReportStore: authDeliveryReportStore,
		ClientTelemetryStore:    clientTelemetryStore,
		ContactStore:            contactStore,
		DialogStore:             dialogStore,
		ChatlistStore:           chatlistStore,
		MessageStore:            messageStore,
		BroadcastStore:          broadcastStore,
		ChannelStore:            channelStore,
		GroupCallStore:          groupCallStore,
		HelpStore:               helpStore,
		LangPackStore:           langPackStore,
		BotStore:                botStore,
		UserCache:               userCache,
		TempAuthKeyStore:        tempAuthKeyStore,
		InlineRegistryStore:     inlineRegistryStore,
		CodeStore:               codeStore,
		RateLimiter:             rateLimiter,
		PasswordStore:           passwordStore,
		SecretChatStore:         secretChatStore,
		StoryStore:              storyStore,
		StarGiftStore:           starGiftStore,
		StarsStore:              starsStore,
		ThemesStore:             themesStore,
		VerificationStore:       verificationStore,
		BotVerificationStore:    botVerificationStore,
		PasskeyStore:            passkeyStore,
		TelegramLoginStore:      telegramLoginStore,
		PollStore:               pollStore,
		PrivacyStore:            privacyStore,
		AccountRatingStore:      accountRatingStore,
		AIStore:                 aiStore,
		AccountLifecycleStore:   accountLifecycleStore,
	}
}

func newPostgresBundle(pool *pgxpool.Pool, rdb *redis.Client, cfg *config.Config, logger *zap.Logger) *store.Bundle {
	authKeyStore := postgres.NewAuthKeyStore(pool)
	userStore := postgres.NewUserStore(pool)
	authzStore := postgres.NewAuthorizationStore(pool)
	adminStore := postgres.NewAdminStore(pool)
	updateStateStore := postgres.NewUpdateStateStore(pool)
	updateEventStore := postgres.NewUpdateEventStore(pool, postgres.WithUpdateEventLogger(logger.Named("store").Named("updates")))
	phoneChangeStore := postgres.NewPhoneChangeStore(pool)
	collectiblePhoneStore := postgres.NewCollectiblePhoneStore(pool)
	readModelVersionStore := postgres.NewReadModelVersionStore(pool)
	dispatchOutboxStore := postgres.NewDispatchOutboxStore(pool, postgres.WithLeaseTimeout(cfg.OutboxLeaseTimeout))
	bootstrapUpdateStore := postgres.NewBootstrapUpdateJobStore(pool)
	botAPIUpdateStore := postgres.NewBotAPIUpdateStore(pool)
	botCallbackStore := redisstore.NewBotCallbackRegistryStore(rdb)
	ephemeralStore := redisstore.NewEphemeralMessageStore(rdb)
	ephemeralReportStore := postgres.NewEphemeralReportStore(pool)
	welcomeMessageStore := postgres.NewWelcomeMessageStore(pool)
	moderationReportStore := postgres.NewModerationReportStore(pool)
	authDeliveryReportStore := postgres.NewAuthDeliveryReportStore(pool)
	clientTelemetryStore := postgres.NewClientTelemetryStore(pool)
	contactStore := postgres.NewContactStore(pool)
	dialogStore := postgres.NewDialogStore(pool)
	chatlistStore := postgres.NewChatlistStore(pool)

	boxIDAllocator := redisstore.NewBoxIDAllocator(rdb, postgres.NewMessageBoxCounterSource(pool))
	messageStore := postgres.NewMessageStore(pool,
		postgres.WithMessageAllocators(boxIDAllocator),
		postgres.WithMessageLogger(logger.Named("store").Named("messages")))

	broadcastStore := postgres.NewBroadcastStore(pool)
	channelIDAllocator := redisstore.NewChannelIDAllocator(rdb, postgres.NewChannelIDCounterSource(pool))
	channelMessageIDAllocator := redisstore.NewChannelMessageIDAllocator(rdb, postgres.NewChannelMessageIDCounterSource(pool))
	channelStore := postgres.NewChannelStore(pool,
		postgres.WithChannelAllocators(channelIDAllocator, channelMessageIDAllocator),
		postgres.WithChannelLogger(logger.Named("store").Named("channels")))

	groupCallStore := postgres.NewGroupCallStore(pool)
	helpStore := postgres.NewHelpStore(pool)
	langPackStore := postgres.NewLangPackStore(pool)
	botStore := postgres.NewBotStore(pool)
	userCache := redisstore.NewUserCache(rdb, redisstore.DefaultUserCacheTTL)
	tempAuthKeyStore := postgres.NewTempAuthKeyBindingStore(pool)
	inlineRegistryStore := redisstore.NewInlineRegistryStore(rdb)
	codeStore := redisstore.NewCodeStore(rdb)
	rateLimiter := redisstore.NewRateLimiter(rdb)
	passwordStore := postgres.NewPasswordStore(pool)
	secretChatStore := postgres.NewSecretChatStore(pool)
	storyStore := postgres.NewStoryStore(pool)
	starGiftStore := postgres.NewStarGiftStore(pool)
	starsStore := postgres.NewStarsStore(pool)
	themesStore := postgres.NewThemeStore(pool)
	verificationStore := postgres.NewVerificationStore(pool)
	botVerificationStore := postgres.NewBotVerificationStore(pool)
	passkeyStore := postgres.NewPasskeyStore(pool)
	telegramLoginStore := postgres.NewTelegramLoginStore(pool)
	pollStore := postgres.NewPollStore(pool)
	privacyStore := postgres.NewPrivacyStore(pool)
	accountRatingStore := postgres.NewAccountRatingStore(pool)
	aiStore := postgres.NewAIComposeStore(pool)
	accountLifecycleStore := postgres.NewAccountLifecycleStore(pool)

	return &store.Bundle{
		IsMemory:                false,
		AuthKeyStore:            authKeyStore,
		UserStore:               userStore,
		AuthzStore:              authzStore,
		AdminCommands:           adminStore,
		AdminRestrictions:       adminStore,
		UpdateStateStore:        updateStateStore,
		UpdateEventStore:        updateEventStore,
		PhoneChangeStore:        phoneChangeStore,
		CollectiblePhoneStore:   collectiblePhoneStore,
		ReadModelVersionStore:   readModelVersionStore,
		DispatchOutboxStore:     dispatchOutboxStore,
		BootstrapUpdateStore:    bootstrapUpdateStore,
		BotAPIUpdateStore:       botAPIUpdateStore,
		BotCallbackStore:        botCallbackStore,
		EphemeralStore:          ephemeralStore,
		EphemeralReportStore:    ephemeralReportStore,
		WelcomeMessageStore:     welcomeMessageStore,
		ModerationReportStore:   moderationReportStore,
		AuthDeliveryReportStore: authDeliveryReportStore,
		ClientTelemetryStore:    clientTelemetryStore,
		ContactStore:            contactStore,
		DialogStore:             dialogStore,
		ChatlistStore:           chatlistStore,
		MessageStore:            messageStore,
		BroadcastStore:          broadcastStore,
		ChannelStore:            channelStore,
		GroupCallStore:          groupCallStore,
		HelpStore:               helpStore,
		LangPackStore:           langPackStore,
		BotStore:                botStore,
		UserCache:               userCache,
		TempAuthKeyStore:        tempAuthKeyStore,
		InlineRegistryStore:     inlineRegistryStore,
		CodeStore:               codeStore,
		RateLimiter:             rateLimiter,
		PasswordStore:           passwordStore,
		SecretChatStore:         secretChatStore,
		StoryStore:              storyStore,
		StarGiftStore:           starGiftStore,
		StarsStore:              starsStore,
		ThemesStore:             themesStore,
		VerificationStore:       verificationStore,
		BotVerificationStore:    botVerificationStore,
		PasskeyStore:            passkeyStore,
		TelegramLoginStore:      telegramLoginStore,
		PollStore:               pollStore,
		PrivacyStore:            privacyStore,
		AccountRatingStore:      accountRatingStore,
		AIStore:                 aiStore,
		AccountLifecycleStore:   accountLifecycleStore,
	}
}

func maskDSN(dsn string) string {
	if len(dsn) < 15 {
		return dsn
	}
	parts := strings.Split(dsn, "@")
	if len(parts) == 2 {
		return "postgres://***:***@" + parts[1]
	}
	return dsn
}
