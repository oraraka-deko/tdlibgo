# Completed Future Plans:
- [x] **Queue manager for downloads and uploads with support of setting limits, start and end date time, active days, and post-completion actions (Sleep/Shutdown/Notify/Transcribe)**
  - Backend: `internal/services/queue_manager.go` (bbolt persistence, speed throttling, scheduled windows)
  - Unit Tests: `internal/services/queue_manager_test.go` (100% passing)
  - REST & WebSocket API: `internal/server/server.go` (`/api/queue/*`)
  - UI Drawer & Modal: `web/index.html`, `web/app.js`, `web/app.css`
- [x] **Extra OAuth for serving app on remote server / public address (not behind NAT)**
  - Stateless JWT sessions, Google & GitHub providers, CSRF/PKCE, HttpOnly 30-day refresh cookies: `internal/auth/oauth.go`, `internal/server/server.go`
- [x] **Full implantation of all 1,745 source files, 35 domain apps, and Zero-Docker in-memory server engine**
  - Standalone in-memory storage engine: `internal/store/memory/` and `internal/storebundle/`
  - Unified CLI: `cmd/tdlibgo/main.go`

# Remaining Research & Future Explorations:
- [ ] Awesome feat with freebox...


  D:\workspace\td\tg\tl_bots_get_popular_app_bots_gen.go (2 hits)
	Line  35: // Fetch popular Main Mini Apps¹, to be used in the apps tab of global search »².
	Line 203: // Fetch popular Main Mini Apps¹, to be used in the apps tab of global search »².
  D:\workspace\td\tg\tl_bots_popular_app_bots_gen.go (1 hit)
	Line  35: // Popular Main Mini Apps¹, to be used in the apps tab of global search »².
  D:\workspace\td\tg\tl_messages_search_global_gen.go (1 hit)
	Line  67: 	// Global search filter

      
  D:\workspace\td\tg\tl_channels_check_search_posts_flood_gen.go (2 hits)
	Line  35: // Check if the specified global post search »¹ requires payment.
	Line 210: // Check if the specified global post search »¹ requires payment.
  D:\workspace\td\tg\tl_channels_search_posts_gen.go (1 hit)
	Line  82: 	// For full text post searches (query), allows payment of the specified amount of Stars
  D:\workspace\td\tg\tl_messages_messages_gen.go (1 hit)
	Line  370: 	// For global post searches »¹, the remaining amount of free searches, here
  D:\workspace\td\tg\tl_search_posts_flood_gen.go (1 hit)
	Line  35: // Indicates if the specified global post search »¹ requires payment.
  D:\workspace\td\tg\tl_stars_transaction_gen.go (1 hit)
	Line   77: 	// Represents payment for a paid global post search »¹.

   