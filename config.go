package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/EcrazerDev/super-mario-maker/globals"
	"github.com/EcrazerDev/super-mario-maker/smmdatabase"
	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	"github.com/PretendoNetwork/plogger-go"
	"github.com/joho/godotenv"
)

func configure() error {
	globals.Logger = plogger.NewLogger()
	_ = godotenv.Load()

	if password := strings.TrimSpace(os.Getenv("PN_KERBEROS_PASSWORD")); password != "" {
		globals.KerberosPassword = password
	}
	globals.AuthenticationServerAccount = nex.NewAccount(types.NewPID(1), "Quazal Authentication", globals.KerberosPassword)
	globals.SecureServerAccount = nex.NewAccount(types.NewPID(2), "Quazal Rendez-Vous", globals.KerberosPassword)

	var err error
	globals.NEXTokenAESKey, err = decodeHexKey("PN_NEX_TOKEN_AES_KEY", 32)
	if err != nil {
		return err
	}
	globals.NEXPasswordSecret, err = decodeHexKey("PN_NEX_PASSWORD_SECRET", 32)
	if err != nil {
		return err
	}

	for _, name := range []string{
		"PN_SMM_POSTGRES_URI",
		"PN_SMM_AUTH_PORT",
		"PN_SMM_SECURE_HOST",
		"PN_SMM_SECURE_PORT",
		"PN_S3_ENDPOINT",
		"PN_S3_ACCESS_KEY",
		"PN_S3_SECRET_KEY",
		"PN_S3_BUCKET",
	} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for _, name := range []string{"PN_SMM_AUTH_PORT", "PN_SMM_SECURE_PORT"} {
		port, parseErr := strconv.Atoi(os.Getenv(name))
		if parseErr != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%s must be a port between 1 and 65535", name)
		}
	}

	smmdatabase.ConnectPostgres()
	return nil
}

func decodeHexKey(name string, minimumBytes int) ([]byte, error) {
	value := strings.TrimSpace(os.Getenv(name))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) < minimumBytes {
		return nil, fmt.Errorf("%s must contain at least %d bytes encoded as hexadecimal", name, minimumBytes)
	}
	return decoded, nil
}
