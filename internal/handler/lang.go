package handler

import (
	"net/http"

	"autorepairos/internal/i18n"
)

// SetLang stores the requested locale in the "lang" cookie and reloads the
// current page. Plain form posts get a redirect; HTMX requests are swapped
// back to the same URL (hx-on-load reload is handled by the caller page).
func SetLang(w http.ResponseWriter, r *http.Request) {
	lang := i18n.Normalize(r.FormValue("lang"))
	if lang == "" {
		lang = i18n.Normalize(r.FormValue("locale"))
	}
	if lang == "" {
		http.Error(w, "不支援的語系", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     i18n.CookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	if r.Header.Get("HX-Request") == "true" {
		// HTMX callers reload the page themselves (see Layout switcher);
		// answer with a no-content so htmx keeps the current URL.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// SetTheme stores the requested UI theme in the "theme" cookie so SSR can
// render <html data-theme> without FOUC. Called via HTMX from the toggle.
func SetTheme(w http.ResponseWriter, r *http.Request) {
	theme := r.FormValue("theme")
	if theme != "light" && theme != "dark" {
		http.Error(w, "不支援的主題", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    theme,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ThemeOf reads the theme cookie; default is "dark" (the historic UI).
func ThemeOf(r *http.Request) string {
	if r == nil {
		return "dark"
	}
	if c, err := r.Cookie("theme"); err == nil && (c.Value == "light" || c.Value == "dark") {
		return c.Value
	}
	return "dark"
}
