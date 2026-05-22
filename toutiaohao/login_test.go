package toutiaohao

import "testing"

func TestIsLoginSuccessURL(t *testing.T) {
	successURLs := []string{
		"https://www.toutiao.com/?is_new_connect=1",
		"https://mp.toutiao.com/profile_v4/index",
		"https://creator.toutiao.com/dashboard",
		"https://mp.toutiao.com/dashboard",
	}

	for _, url := range successURLs {
		if !IsLoginSuccessURL(url) {
			t.Errorf("IsLoginSuccessURL(%q) = false, want true", url)
		}
	}

	failURLs := []string{
		"https://mp.toutiao.com/auth/page/login/",
		"https://mp.toutiao.com/auth/page/login?redirect=xxx",
		"https://www.google.com",
		"",
	}

	for _, url := range failURLs {
		if IsLoginSuccessURL(url) {
			t.Errorf("IsLoginSuccessURL(%q) = true, want false", url)
		}
	}
}

func TestSelectorsNotEmpty(t *testing.T) {
	selectorLists := map[string][]string{
		"LoginUsernameSelectors":    LoginUsernameSelectors,
		"LoginPasswordSelectors":    LoginPasswordSelectors,
		"LoginSubmitButtonSelectors": LoginSubmitButtonSelectors,
	}

	for name, selList := range selectorLists {
		if len(selList) == 0 {
			t.Errorf("Selector list %s is empty", name)
		}
		for i, sel := range selList {
			if sel == "" {
				t.Errorf("Selector %s[%d] is empty", name, i)
			}
		}
	}
}

func TestValidateLoginRequest_EmptyUsername(t *testing.T) {
	err := ValidateLoginRequest("", "password123")
	if err == nil {
		t.Error("expected error for empty username")
	}
	if err.Error() != "username is required" {
		t.Errorf("error = %q, want %q", err.Error(), "username is required")
	}
}

func TestValidateLoginRequest_EmptyPassword(t *testing.T) {
	err := ValidateLoginRequest("user", "")
	if err == nil {
		t.Error("expected error for empty password")
	}
	if err.Error() != "password is required" {
		t.Errorf("error = %q, want %q", err.Error(), "password is required")
	}
}

func TestValidateLoginRequest_Valid(t *testing.T) {
	err := ValidateLoginRequest("user", "pass")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
