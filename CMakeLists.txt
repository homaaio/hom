cmake_minimum_required(VERSION 3.15)
project(hom LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# hom is Windows-only for now (WinHTTP, CNG, the Win32 console).
#   cmake -S . -B build -G "MinGW Makefiles"      (MSYS2 MINGW64 shell)
#   cmake --build build                           -> build/hom.exe
if(NOT WIN32)
    message(FATAL_ERROR "hom builds on Windows only (MinGW-w64 or MSVC).")
endif()

# Plain `main`, not wmain: no -municode.
add_executable(hom src/hom.cpp)
target_compile_definitions(hom PRIVATE UNICODE _UNICODE)

if(MSVC)
    target_compile_options(hom PRIVATE /W3)
else()
    target_compile_options(hom PRIVATE -O2 -Wall -Wextra)
    # -static: a copied hom.exe runs without the MinGW runtime DLLs.
    target_link_options(hom PRIVATE -static -static-libgcc -static-libstdc++)
endif()

target_link_libraries(hom PRIVATE winhttp shlwapi dbghelp bcrypt shell32)
