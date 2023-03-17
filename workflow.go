package app

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func MoneyTransfer(ctx workflow.Context, input PaymentDetails) (string, error) {

	// RetryPolicy specifies how to automatically handle retries if an Activity fails.
	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:        time.Second,
		BackoffCoefficient:     2.0,
		MaximumInterval:        100 * time.Second,
		MaximumAttempts:        0, // unlimited retries
		NonRetryableErrorTypes: []string{"InvalidAccountError", "InsufficientFundsError"},
	}

	options := workflow.ActivityOptions{
		// Timeout options specify when to automatically timeout Activity functions.
		StartToCloseTimeout: time.Minute,
		// Optionally provide a customized RetryPolicy.
		// Temporal retries failed Activities by default.
		RetryPolicy: retrypolicy,
	}

	// Apply the options.
	ctx = workflow.WithActivityOptions(ctx, options)

	// Withdraw money.
	var withdrawOutput string
	withdrawErr := workflow.ExecuteActivity(ctx, Withdraw, input).Get(ctx, &withdrawOutput)
	if withdrawErr != nil {
		return "", withdrawErr
	}

	// Deposit money.
	var depositOutput string
	depositErr := workflow.ExecuteActivity(ctx, Deposit, input).Get(ctx, &depositOutput)
	if depositErr != nil {
		// The deposit failed; put money back in original account.
		var result string
		refundErr := workflow.ExecuteActivity(ctx, Refund, input).Get(ctx, &result)
		if refundErr != nil {
			return "",
				fmt.Errorf("Deposit: failed to deposit money into %v: %v. Money could not be returned to %v: %w",
					input.TargetAccount, depositErr, input.SourceAccount, refundErr)
		}

		return "", fmt.Errorf("Deposit: failed to deposit money into %v: Money returned to %v: %w",
			input.TargetAccount, input.SourceAccount, depositErr)
	}

	result := fmt.Sprintf("Transfer complete (transaction IDs: %s, %s)", withdrawOutput, depositOutput)
	return result, nil
}

// ParseClientOptionFlags parses the given arguments into client options
func ParseClientOptionFlags(args []string) (client.Options, error) {
	set := flag.NewFlagSet("money-transfer", flag.ExitOnError)

	address := set.String("address", os.Getenv("TEMPORAL_ADDRESS"), "host:port for Temporal frontend service [$TEMPORAL_ADDRESS]")
	namespace := set.String("namespace", os.Getenv("TEMPORAL_NAMESPACE"), "Temporal workflow namespace [$TEMPORAL_NAMESPACE]")
	clientCert := set.String("tls_cert_path", os.Getenv("TEMPORAL_CLIENT_TLS_CERT"), "Path to client x509 certificate [$TEMPORAL_CLIENT_TLS_CERT]")
	clientKey := set.String("tls_key_path", os.Getenv("TEMPORAL_CLIENT_TLS_KEY"), "Path to client certificate private key [$TEMPORAL_CLIENT_TLS_KEY]")

	if err := set.Parse(args); err != nil {
		return client.Options{}, fmt.Errorf("failed parsing args: %w", err)
	}

	options := client.Options{
		HostPort:  *address,
		Namespace: *namespace,
	}

	if *clientCert != "" && *clientKey != "" {
		// Load client cert
		cert, err := tls.LoadX509KeyPair(*clientCert, *clientKey)
		if err != nil {
			return client.Options{}, fmt.Errorf("failed loading client cert and key: %w", err)
		}

		options.ConnectionOptions = client.ConnectionOptions{
			TLS: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}
	}

	return options, nil
}
