package retained

import (
	"fmt"
	"strings"
	"testing"
)

func configurationFixture() ([]byte, []byte) {
	api := "TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("ab", 16) + "\n"
	bridge := api + "BRIDGE_ENCRYPTION_KEY=" + strings.Repeat("00", 32) + "\n" +
		"TELEGRAM_OWNER_ID=101\nTELEGRAM_MANAGEMENT_CHAT_ID=-100\nTELEGRAM_MASTER_TOKEN=11:" + strings.Repeat("x", 32) + "\n" +
		"BRIDGE_ADMIN_TOKEN=" + strings.Repeat("admin", 8) + "\nOPENWA_WEBHOOK_SECRET=" + strings.Repeat("hook", 8) + "\n" +
		"LITERAL_VALUE='a $value with spaces'\n"
	return []byte(bridge), []byte(api)
}

func TestConfigurationPreservesLiteralIdentityAndRedaction(t *testing.T) {
	bridge, telegram := configurationFixture()
	c, err := ParseConfiguration(bridge, telegram)
	if err != nil || c.OwnerID() != 101 || c.MasterID() != 11 || c.ManagementChatID() != -100 || c.EncryptionKey() != [32]byte{} {
		t.Fatal("retained identity changed", err)
	}
	if c.Value("LITERAL_VALUE") != "'a $value with spaces'" {
		t.Fatal("literal configuration was interpreted")
	}
	for _, formatted := range []string{fmt.Sprint(c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(formatted, c.Value("TELEGRAM_MASTER_TOKEN")) || strings.Contains(formatted, c.Value("BRIDGE_ADMIN_TOKEN")) {
			t.Fatal("configuration formatting disclosed private values")
		}
	}
}

func TestConfigurationRejectsDamagedOrAmbiguousIdentity(t *testing.T) {
	bridge, telegram := configurationFixture()
	for _, raw := range [][]byte{
		append(append([]byte{}, bridge...), []byte("TELEGRAM_OWNER_ID=102\n")...),
		[]byte(strings.Replace(string(bridge), "TELEGRAM_OWNER_ID=101", "TELEGRAM_OWNER_ID=0", 1)),
		[]byte(strings.Replace(string(bridge), "TELEGRAM_API_ID=12345", "TELEGRAM_API_ID=999", 1)),
		[]byte(strings.Replace(string(bridge), "TELEGRAM_MANAGEMENT_CHAT_ID=-100", "TELEGRAM_MANAGEMENT_CHAT_ID=100", 1)),
	} {
		if _, err := ParseConfiguration(raw, telegram); err == nil {
			t.Fatal("invalid retained configuration accepted")
		}
	}
}
