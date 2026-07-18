package nfc

import "testing"

func TestTokenResponseRoundTrip(t *testing.T) {
	token := "nfc1.payload.signature"
	response, err := BuildTokenResponse(token)
	if err != nil {
		t.Fatalf("BuildTokenResponse() error = %v", err)
	}
	got, err := ParseTokenResponse(response)
	if err != nil {
		t.Fatalf("ParseTokenResponse() error = %v", err)
	}
	if got != token {
		t.Fatalf("token mismatch: %q != %q", got, token)
	}
}

func TestParseTokenResponseRejectsNonSuccess(t *testing.T) {
	if _, err := ParseTokenResponse([]byte{0x70, 0x00, 0x6F, 0x00}); err == nil {
		t.Fatal("expected non-success status to fail")
	}
}
