// Package ratelimit protects the public authentication boundary with bounded,
// process local token buckets. It keeps caller material hashed and never sends
// a denied request to identity (spec 0007).
package ratelimit
