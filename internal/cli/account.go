package cli

import (
	"time"

	"github.com/matteodante/miniform/internal/accounts"
)

type accountView struct {
	ID          uint       `json:"id"`
	Email       string     `json:"email"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (r *Runner) runAccount(args []string) (any, error) {
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	action, actionArgs, err := requireAction("account", args)
	if err != nil {
		return nil, err
	}

	switch action {
	case "show":
		return r.accountShow(actionArgs)
	case "set-email":
		return r.accountSetEmail(actionArgs)
	case "change-password":
		return r.accountChangePassword(actionArgs)
	case "reset-password":
		return r.accountResetPassword(actionArgs)
	default:
		return nil, usageError("unknown account action: " + action)
	}
}

func (r *Runner) accountShow(args []string) (any, error) {
	set := newFlagSet("account.show")
	if err := r.parseFlags(set, "account.show", args); err != nil {
		return nil, err
	}
	user, err := accounts.GetAdmin(r.DB)
	if err != nil {
		return nil, err
	}
	return newAccountView(user), nil
}

func (r *Runner) accountSetEmail(args []string) (any, error) {
	set := newFlagSet("account.set-email")
	email := set.String("email", "", "new operator email")
	passwordFile := set.String("current-password-file", "", "path containing current password, or - for stdin")
	if err := r.parseFlags(set, "account.set-email", args); err != nil {
		return nil, err
	}
	if err := requireString(*email, "email"); err != nil {
		return nil, err
	}
	if err := requireString(*passwordFile, "current-password-file"); err != nil {
		return nil, err
	}

	password, err := readFileValue(*passwordFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	user, err := accounts.GetAdmin(r.DB)
	if err != nil {
		return nil, err
	}
	if err := accounts.ChangeEmail(r.Logger, r.DB, user.Email, *email, password); err != nil {
		return nil, err
	}
	updated, err := accounts.GetAdmin(r.DB)
	if err != nil {
		return nil, err
	}
	return newAccountView(updated), nil
}

func (r *Runner) accountChangePassword(args []string) (any, error) {
	set := newFlagSet("account.change-password")
	currentFile := set.String("current-password-file", "", "path containing current password")
	newFile := set.String("new-password-file", "", "path containing new password")
	if err := r.parseFlags(set, "account.change-password", args); err != nil {
		return nil, err
	}
	if err := validateTwoSecretFiles(*currentFile, *newFile); err != nil {
		return nil, err
	}

	currentPassword, err := readFileValue(*currentFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	newPassword, err := readFileValue(*newFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	user, err := accounts.GetAdmin(r.DB)
	if err != nil {
		return nil, err
	}
	if err := accounts.ChangePassword(r.Logger, r.DB, user.Email, currentPassword, newPassword); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true, "account_id": user.ID}, nil
}

func (r *Runner) accountResetPassword(args []string) (any, error) {
	set := newFlagSet("account.reset-password")
	email := set.String("email", "", "operator email; defaults to the local operator account")
	newFile := set.String("new-password-file", "", "path containing new password, or - for stdin")
	if err := r.parseFlags(set, "account.reset-password", args); err != nil {
		return nil, err
	}
	if err := requireString(*newFile, "new-password-file"); err != nil {
		return nil, err
	}

	newPassword, err := readFileValue(*newFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if *email == "" {
		user, err := accounts.GetAdmin(r.DB)
		if err != nil {
			return nil, err
		}
		*email = user.Email
	}
	if err := accounts.ResetPassword(r.Logger, r.DB, *email, newPassword); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true, "email": *email}, nil
}

func validateTwoSecretFiles(first, second string) error {
	if err := requireString(first, "current-password-file"); err != nil {
		return err
	}
	if err := requireString(second, "new-password-file"); err != nil {
		return err
	}
	if first == "-" && second == "-" {
		return usageError("only one password file can be stdin; provide a file path for the other password")
	}
	return nil
}

func newAccountView(user *accounts.User) accountView {
	return accountView{
		ID:          user.ID,
		Email:       user.Email,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
