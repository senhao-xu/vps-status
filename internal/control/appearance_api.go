package control

import (
	"errors"
	"net/http"

	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

// appearanceDTO is also the explicit public presentation allowlist.
type appearanceDTO struct {
	DefaultTheme string `json:"default_theme"`
}

func (api *adminAPI) publicAppearance(response http.ResponseWriter, request *http.Request) {
	appearance, err := api.config.Store.AppearanceConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, appearanceDTO{DefaultTheme: appearance.DefaultTheme})
}

func (api *adminAPI) appearance(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	api.publicAppearance(response, request)
}

func (api *adminAPI) updateAppearance(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input appearanceDTO
	if !decodeJSON(response, request, &input) {
		return
	}
	err := api.config.Store.UpdateAppearance(request.Context(), controlstore.Appearance{DefaultTheme: input.DefaultTheme})
	if errors.Is(err, controlstore.ErrInvalidAppearance) {
		writeError(response, http.StatusBadRequest, "invalid_input", "Choose a supported built-in theme.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, input)
}
