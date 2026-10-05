// Package model holds the user module's entities and value objects.
//
// The domain validates structure only. Whether a province or ward code exists
// in the official dataset is checked in the application layer through the
// Divisions port, because Constitution I allows this package to import the
// standard library and share/access and nothing else.
package model
