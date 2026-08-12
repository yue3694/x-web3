package config

import "testing"

func setValidBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("PRIVY_APP_ID", "app-test")
	t.Setenv("PRIVY_JWKS_URL", "https://example.test/jwks.json")
	t.Setenv("PRIVY_DEV_STUB", "")
	t.Setenv("PRIVY_DEV_STUB_SUBJECT", "")
	t.Setenv("OBJECT_STORE_BUCKET", "test-media")
}

func TestLoadRejectsDevStubOutsideDevelopment(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("API_ENV", "staging")
	t.Setenv("PRIVY_DEV_STUB", "1")
	t.Setenv("PRIVY_DEV_STUB_SUBJECT", "did:privy:dev")

	if _, err := Load(); err == nil {
		t.Fatal("expected staging configuration to reject Privy dev stub")
	}
}

func TestLoadRequiresSecureCookieInProduction(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("API_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected production configuration to require secure cookie")
	}
}

func TestLoadAcceptsSecureProductionConfiguration(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("API_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.CookieSecure {
		t.Fatal("expected CookieSecure")
	}
}

func TestLoadUsesConfiguredTestChain(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("CHAIN_ID", "31337")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainID != 31337 {
		t.Fatalf("ChainID = %d, want 31337", cfg.ChainID)
	}
}
