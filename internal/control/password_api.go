package control

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type changePasswordRequest struct {
	CurrentPassword *string `json:"current_password"`
	NewPassword     *string `json:"new_password"`
}

func (api *adminAPI) changePassword(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	if !api.allowKey(response, api.config.LoginLimiter, "password:"+strconv.FormatInt(auth.admin.ID, 10)) {
		return
	}
	var input changePasswordRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.CurrentPassword == nil || input.NewPassword == nil {
		writeError(response, http.StatusBadRequest, "invalid_input", "Current and new passwords are required.")
		return
	}
	passwordOK, err := security.VerifyPassword(*input.CurrentPassword, auth.admin.PasswordHash)
	if err != nil {
		writeInternal(response)
		return
	}
	if !passwordOK {
		writeError(response, http.StatusBadRequest, "current_password_invalid", "The current password is invalid.")
		return
	}
	hash, err := security.HashPassword(*input.NewPassword, api.config.PasswordParams)
	if err != nil {
		writeInternal(response)
		return
	}
	if err := api.config.Store.ChangePassword(request.Context(), auth.admin.ID, auth.session.ID, auth.admin.PasswordHash, hash, auth.admin.TOTPConfirmed, api.now()); err != nil {
		if errors.Is(err, controlstore.ErrAdminStateChanged) {
			writeError(response, http.StatusConflict, "auth_state_changed", "Authentication settings changed. Try again.")
			return
		}
		writeInternal(response)
		return
	}
	api.clearCookies(response)
	response.WriteHeader(http.StatusNoContent)
}
