include(ExternalProject)
set(TUNNEL_STAGE "${CMAKE_CURRENT_BINARY_DIR}/tunnel-deps")
file(MAKE_DIRECTORY "${TUNNEL_STAGE}/include")
set(TUNNEL_COMMON_ARGS
  "-DCMAKE_TOOLCHAIN_FILE=${CMAKE_TOOLCHAIN_FILE}"
  "-DCMAKE_MAKE_PROGRAM=${CMAKE_MAKE_PROGRAM}"
  "-DCMAKE_BUILD_TYPE=Release"
  "-DCMAKE_POSITION_INDEPENDENT_CODE=ON"
  "-DCMAKE_INSTALL_PREFIX=${TUNNEL_STAGE}"
  "-DOHOS_ARCH=${OHOS_ARCH}"
  "-DOHOS_STL=c++_static")
foreach(SDK_VARIABLE OHOS_SDK_NATIVE HMOS_SDK_NATIVE)
  if(DEFINED ${SDK_VARIABLE})
    file(TO_CMAKE_PATH "${${SDK_VARIABLE}}" SDK_PATH)
    list(APPEND TUNNEL_COMMON_ARGS "-D${SDK_VARIABLE}=${SDK_PATH}")
  endif()
endforeach()
ExternalProject_Add(tunnel_mbedtls
  URL https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-3.6.7/mbedtls-3.6.7.tar.bz2
  URL_HASH SHA256=a7e8bcbec0e6f761b4af24f25677626b35f762f68eef79c08677a363212d11f6
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE
  CMAKE_ARGS ${TUNNEL_COMMON_ARGS} -DENABLE_PROGRAMS=OFF -DENABLE_TESTING=OFF -DGEN_FILES=OFF
    -DUSE_SHARED_MBEDTLS_LIBRARY=OFF -DCMAKE_C_FLAGS=-Wno-unused-command-line-argument
  BUILD_BYPRODUCTS "${TUNNEL_STAGE}/lib/libmbedcrypto.a" "${TUNNEL_STAGE}/lib/libmbedx509.a"
    "${TUNNEL_STAGE}/lib/libmbedtls.a" "${TUNNEL_STAGE}/lib/libeverest.a" "${TUNNEL_STAGE}/lib/libp256m.a")
ExternalProject_Add(tunnel_nghttp2
  URL https://codeload.github.com/nghttp2/nghttp2/tar.gz/refs/tags/v1.71.0
  URL_HASH SHA256=81c3702fdaee37145c875cb6d1288698e4f755558341fc85335d9750fb718f33
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE
  CMAKE_ARGS ${TUNNEL_COMMON_ARGS} -DENABLE_LIB_ONLY=ON -DBUILD_SHARED_LIBS=OFF
    -DBUILD_STATIC_LIBS=ON -DBUILD_TESTING=OFF -DENABLE_DOC=OFF
  BUILD_BYPRODUCTS "${TUNNEL_STAGE}/lib/libnghttp2.a")
ExternalProject_Add(tunnel_ssh2
  URL https://codeload.github.com/libssh2/libssh2/tar.gz/refs/tags/libssh2-1.11.1
  URL_HASH SHA256=82b35c61c78b475647bdc981a183c5b5ab0d979e1caee94186e8f9150f2b0d0d
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE
  DEPENDS tunnel_mbedtls
  CMAKE_ARGS ${TUNNEL_COMMON_ARGS} -DCRYPTO_BACKEND=mbedTLS
    "-DMBEDTLS_INCLUDE_DIR=${TUNNEL_STAGE}/include"
    "-DMBEDCRYPTO_LIBRARY=${TUNNEL_STAGE}/lib/libmbedcrypto.a"
    -DBUILD_SHARED_LIBS=OFF -DBUILD_STATIC_LIBS=ON -DBUILD_TESTING=OFF -DBUILD_EXAMPLES=OFF
    -DENABLE_ZLIB_COMPRESSION=OFF
  BUILD_BYPRODUCTS "${TUNNEL_STAGE}/lib/libssh2.a")
include(FetchContent)
FetchContent_Declare(tunnel_json
  URL https://codeload.github.com/nlohmann/json/tar.gz/refs/tags/v3.12.0
  URL_HASH SHA256=4b92eb0c06d10683f7447ce9406cb97cd4b453be18d7279320f7b2f025c10187
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE)
FetchContent_GetProperties(tunnel_json)
if(NOT tunnel_json_POPULATED)
  FetchContent_Populate(tunnel_json)
endif()
set(TUNNEL_LIBRARIES
  "${TUNNEL_STAGE}/lib/libssh2.a"
  "${TUNNEL_STAGE}/lib/libmbedtls.a" "${TUNNEL_STAGE}/lib/libmbedx509.a"
  "${TUNNEL_STAGE}/lib/libmbedcrypto.a" "${TUNNEL_STAGE}/lib/libnghttp2.a"
  "${TUNNEL_STAGE}/lib/libeverest.a" "${TUNNEL_STAGE}/lib/libp256m.a")
set(TUNNEL_CA_FILE "${CMAKE_CURRENT_BINARY_DIR}/cacert-2026-11-05.pem")
file(DOWNLOAD https://curl.se/ca/cacert-2026-11-05.pem "${TUNNEL_CA_FILE}"
  EXPECTED_HASH SHA256=427c064216761a1e72a8b30f0d954d08cb6e4435a82091fb865afd20c8497285
  TLS_VERIFY ON)
file(READ "${TUNNEL_CA_FILE}" TUNNEL_CA_TEXT)
configure_file("${CMAKE_CURRENT_LIST_DIR}/trusted_ca.h.in" "${CMAKE_CURRENT_BINARY_DIR}/trusted_ca.h" @ONLY)
