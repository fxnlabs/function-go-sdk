package test

import (
	"errors"
	sdk "github.com/fxnlabs/function-go-sdk"
	"testing"
)

func TestCreateClient(t *testing.T) {
	client, err := sdk.NewClient(sdk.ClientOptions{
		ApiKey: "mykey",
	})

	if err != nil {
		t.Fatalf("Client creation failed with error %v", err)
	}
	if client == nil {
		t.Fatalf("There was no error, but client was nil")
	}
}

func TestCreateClientWithoutApiKey(t *testing.T) {
	client, err := sdk.NewClient(sdk.ClientOptions{})

	if err == nil {
		t.Fatalf("No error was thrown")
	}
	if !errors.Is(err, sdk.MissingApiKeyError) {
		t.Fatalf("Expected MissingApiKeyError, got %v", err)
	}

	if client != nil {
		t.Fatalf("Expected nil client")
	}
}
