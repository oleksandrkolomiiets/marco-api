package auth

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Device details arrive as headers rather than body fields so sign-in, sign-up,
// Google sign-in and refresh can all report them without four request schemas
// growing the same three keys.
const (
	headerDeviceName = "X-Device-Name"
	headerPlatform   = "X-Device-Platform"
	headerAppVersion = "X-App-Version"
)

// Column widths in migration 000019. These are strings a client controls, so
// clamp them here instead of letting Postgres reject the insert.
const (
	maxDeviceNameLen = 120
	maxPlatformLen   = 60
	maxAppVersionLen = 40
)

// clampHeader trims, strips control characters and cuts to a rune budget. The
// value is echoed straight back onto the devices screen, so a client that
// sends a newline or a kilobyte of text shouldn't be able to wreck it.
func clampHeader(v string, maxLen int) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if utf8.RuneCountInString(v) <= maxLen {
		return v
	}
	return string([]rune(v)[:maxLen])
}

func deviceInfoFrom(c *fiber.Ctx) DeviceInfo {
	return DeviceInfo{
		DeviceName: clampHeader(c.Get(headerDeviceName), maxDeviceNameLen),
		Platform:   clampHeader(c.Get(headerPlatform), maxPlatformLen),
		AppVersion: clampHeader(c.Get(headerAppVersion), maxAppVersionLen),
	}
}

// SessionIDFrom reads the session the caller's access token was issued to.
// uuid.Nil means the token predates sessions.
func SessionIDFrom(c *fiber.Ctx) uuid.UUID {
	id, _ := c.Locals("session_id").(uuid.UUID)
	return id
}

type deviceResponse struct {
	ID         string  `json:"id"`
	DeviceName *string `json:"device_name"`
	Platform   *string `json:"platform"`
	AppVersion *string `json:"app_version"`
	SignedInAt string  `json:"signed_in_at"`
	LastSeenAt string  `json:"last_seen_at"`
	// Current marks the device making the request, so the UI can label it and
	// warn before signing itself out.
	Current bool `json:"current"`
}

func toDeviceResponse(s Session, currentID uuid.UUID) deviceResponse {
	return deviceResponse{
		ID:         s.ID.String(),
		DeviceName: s.DeviceName,
		Platform:   s.Platform,
		AppVersion: s.AppVersion,
		SignedInAt: s.CreatedAt.UTC().Format(time.RFC3339),
		LastSeenAt: s.LastSeenAt.UTC().Format(time.RFC3339),
		Current:    currentID != uuid.Nil && s.ID == currentID,
	}
}

// ListDevices returns every device with a live session on this account.
func (h *Handler) ListDevices(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	sessions, err := h.auth.ListSessions(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	current := SessionIDFrom(c)
	devices := make([]deviceResponse, 0, len(sessions))
	for _, s := range sessions {
		devices = append(devices, toDeviceResponse(s, current))
	}
	return c.JSON(fiber.Map{"devices": devices})
}

// RevokeDevice signs one device out. Revoking your own is allowed — it is just
// signing out — but the response says so, so the app knows to clear its tokens
// instead of carrying on with credentials the server has thrown away.
func (h *Handler) RevokeDevice(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid device id"})
	}

	// RevokeSession is scoped by user_id, so an id belonging to someone else
	// reads as "not found" rather than signing a stranger out.
	revoked, err := h.auth.RevokeSession(c.Context(), userID, deviceID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if !revoked {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "device not found"})
	}

	return c.JSON(fiber.Map{"signed_out_self": deviceID == SessionIDFrom(c)})
}

// RevokeOtherDevices is the "it wasn't me" button: everything except the
// device asking loses its session.
func (h *Handler) RevokeOtherDevices(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	current := SessionIDFrom(c)
	if current == uuid.Nil {
		// Without a session claim there is no "other" to scope to, and running
		// it anyway would sign this device out along with the rest.
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "sign in again before signing out other devices",
		})
	}

	count, err := h.auth.RevokeOtherSessions(c.Context(), userID, current)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.JSON(fiber.Map{"signed_out": count})
}
