# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- CUE native constraints: type, regex, enum, range, nested struct, array `[...{schema}]`, nullable `null | type`
- `@blob()` dynamic expressions: Bloblang syntax for validation (bool) and computed fields (non-bool)
- `@meta()` field behavior control: priority, optional, conditional, skip_empty, fail_fast, omit_if_skip, omit_empty, required_if, skip_if
- Three FailModes: `FailAll`, `FailFast`, `FailPriority`
- Structured error codes: `E{layer}{category}{seq}` system
- Bloblang method registration: `Registry.RegisterMethods()` — `this.validate_schema()` / `this.process_schema()`
- Bloblang function registration: `Registry.RegisterFunctions()` — `validate_schema(data: ...)` / `process_schema(data: ...)`
- `RegisterAll()` convenience method for both method + function forms
- Dynamic `data` parameter support via `RegisterAdvancedFunction`
- Optional `mode` parameter: `"all"`, `"fast"`, `"priority"`
- `Registry` management: `Register`, `Get`, `Has`, `Unregister`, `List`, `Len`
- `MustNew()` panic-on-error constructor for package-level initialization
- `NewWithContext()` for shared CUE context across validators
- `Result` convenience methods: `Err()`, `FirstError()`, `ErrorsByPath()`, `ErrorMessages()`
- `ValidationError` implements `error` interface
- Pre-compiled CUE field descriptors for optimized runtime validation
- Recursive multi-level struct and array validation
