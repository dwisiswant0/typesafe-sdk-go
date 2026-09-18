// Package typesafe evaluates content and lists models through the TypeSafe AI API.
//
// Create a [Client] with [NewClient] and reuse it across requests. Pass a context
// to cancel a call or set a deadline for requests and retry waits. Empty string
// fields in [Config] use TYPESAFE_API_KEY, TYPESAFE_BASE_URL, and
// TYPESAFE_DEFAULT_MODEL. [Config.BaseURL] and [Config.DefaultModel] then fall back
// to SDK defaults.
// An API key is required.
//
// [Client.SystemOne] accepts named [NoulQuestion], [ChoiceQuestion],
// [ScoreQuestion], or [RawQuestion] values. Answers use the same names. Use a
// type assertion or switch to read a pointer to [NoulAnswer], [ChoiceAnswer],
// [ScoreAnswer], or [UnknownAnswer].
package typesafe
