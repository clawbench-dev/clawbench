package backends

// ACP mapping data is now registered directly in each backend sub-package's
// init() via backends.Register(&BackendPlugin{...}).
// This file previously held centralized ACP registration via registerACP(),
// which caused import cycles when sub-packages imported the backends package.
// The registerACP() function is removed; all ACP data flows through Register().
