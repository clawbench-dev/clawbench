#include "napi/native_api.h"
#include "tunnel_engine.h"
#include <string>

namespace {
struct Work {
    napi_async_work work = nullptr;
    napi_deferred deferred = nullptr;
    std::string request;
    std::string result;
    uint64_t epoch = 0;
};

napi_value Execute(napi_env env, napi_callback_info info)
{
    size_t argc = 1;
    napi_value argv[1];
    napi_get_cb_info(env, info, &argc, argv, nullptr, nullptr);
    size_t length = 0;
    if (argc != 1 || napi_get_value_string_utf8(env, argv[0], nullptr, 0, &length) != napi_ok || length > 65536) {
        napi_throw_type_error(env, nullptr, "Expected a bounded tunnel command");
        return nullptr;
    }
    auto* work = new Work();
    work->epoch = clawbench::TunnelEpoch();
    work->request.resize(length + 1);
    napi_get_value_string_utf8(env, argv[0], work->request.data(), length + 1, &length);
    work->request.resize(length);
    napi_value promise;
    napi_create_promise(env, &work->deferred, &promise);
    napi_value resource;
    napi_create_string_utf8(env, "ClawBenchTunnel", NAPI_AUTO_LENGTH, &resource);
    napi_status status = napi_create_async_work(env, nullptr, resource,
        [](napi_env, void* data) {
            auto* task = static_cast<Work*>(data);
            task->result = clawbench::ExecuteTunnel(task->request, task->epoch);
        },
        [](napi_env env, napi_status status, void* data) {
            auto* task = static_cast<Work*>(data);
            napi_value result;
            const std::string fallback = "{\"ok\":false,\"error\":\"Native work cancelled\",\"errorType\":\"network\"}";
            const std::string& output = status == napi_ok ? task->result : fallback;
            napi_create_string_utf8(env, output.data(), output.size(), &result);
            napi_resolve_deferred(env, task->deferred, result);
            napi_delete_async_work(env, task->work);
            delete task;
        }, work, &work->work);
    if (status != napi_ok || napi_queue_async_work(env, work->work) != napi_ok) {
        napi_value error;
        napi_create_string_utf8(env, "Unable to queue tunnel command", NAPI_AUTO_LENGTH, &error);
        napi_reject_deferred(env, work->deferred, error);
        if (work->work) napi_delete_async_work(env, work->work);
        delete work;
    }
    return promise;
}

napi_value Cancel(napi_env env, napi_callback_info)
{
    clawbench::CancelTunnel();
    napi_value result;
    napi_get_undefined(env, &result);
    return result;
}

napi_value Init(napi_env env, napi_value exports)
{
    napi_property_descriptor descriptors[] = {
        {"execute", nullptr, Execute, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"cancel", nullptr, Cancel, nullptr, nullptr, nullptr, napi_default, nullptr}
    };
    napi_define_properties(env, exports, sizeof(descriptors) / sizeof(descriptors[0]), descriptors);
    napi_add_env_cleanup_hook(env, [](void*) { clawbench::CancelTunnel(); }, nullptr);
    return exports;
}

static napi_module module = {1, 0, nullptr, Init, "clawbench_tunnel", nullptr, {0}};
extern "C" __attribute__((constructor)) void RegisterClawBenchTunnel()
{
    napi_module_register(&module);
}
}
