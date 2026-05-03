package tasks

// TypeRunCredentialHealthCheck runs the daily credential health cron.
// Validates every Apple + Google connection and tracks cert/profile expiry.
const TypeRunCredentialHealthCheck = "cron:credential-health-daily"
