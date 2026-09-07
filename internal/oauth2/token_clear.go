package oauth2

// Clear releases pending OAuth secrets; immutable string copies may still exist.
func (t *TokenResponse) Clear() {
	if t != nil {
		*t = TokenResponse{}
	}
}
