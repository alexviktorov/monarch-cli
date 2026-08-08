module monarch-cli

go 1.24.7

require (
	golang.org/x/sys v0.28.0
	golang.org/x/term v0.27.0
)

require (
	github.com/eshaffer321/monarch-go/v2 v2.0.0
	github.com/getsentry/sentry-go v0.35.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/text v0.14.0
)

replace golang.org/x/term => github.com/golang/term v0.27.0

replace golang.org/x/sys => github.com/golang/sys v0.28.0

replace golang.org/x/text => github.com/golang/text v0.14.0
