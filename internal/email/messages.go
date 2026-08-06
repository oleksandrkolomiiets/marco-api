package email

import (
	"fmt"
	"strings"
	"time"
)

const (
	passwordResetSubject = "Your Marco password reset code"
	googleAccountSubject = "Signing in to Marco"
)

// greeting keeps the salutation from reading "Hola ," when we have no name —
// display_name is nullable and Google sign-ups can arrive without one.
func greeting(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return "Hola " + n + ","
	}
	return "Hola,"
}

// humanTTL renders the code lifetime the way the email should say it, so the
// copy can never drift from PasswordResetTTL.
func humanTTL(ttl time.Duration) string {
	minutes := int(ttl.Minutes())
	switch {
	case minutes <= 1:
		return "1 minute"
	case minutes < 60:
		return fmt.Sprintf("%d minutes", minutes)
	case minutes == 60:
		return "1 hour"
	default:
		return fmt.Sprintf("%d hours", minutes/60)
	}
}

func passwordResetBody(name, code string, ttl time.Duration) string {
	return fmt.Sprintf(`%s

Here is the code to reset your Marco password:

    %s

Type it into the app on the "Forgot password" screen. It works once and
expires in %s.

If you didn't ask for this, you can ignore this email — your password
hasn't changed.

— Marco
`, greeting(name), code, humanTTL(ttl))
}

func googleAccountBody(name string) string {
	return fmt.Sprintf(`%s

Someone asked to reset the password for this email address, but your Marco
account signs in with Google — there's no password on it to reset.

Open the app and use "Continue with Google" instead.

If that wasn't you, nothing has changed on your account.

— Marco
`, greeting(name))
}
