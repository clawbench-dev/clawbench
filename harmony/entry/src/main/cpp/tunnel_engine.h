#pragma once
#include <cstdint>
#include <string>

namespace clawbench {
// Called before queueing disconnect so in-flight network work observes cancellation immediately.
void CancelTunnel();
uint64_t TunnelEpoch();
std::string ExecuteTunnel(const std::string& command);
std::string ExecuteTunnel(const std::string& command, uint64_t epoch);
}
