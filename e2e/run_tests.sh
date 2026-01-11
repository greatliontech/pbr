#!/bin/bash
# E2E Test Suite for PBR
# This script runs comprehensive end-to-end tests using the buf CLI

set -e

# Configuration
REGISTRY_HOST="${REGISTRY_HOST:-pbr-example.greatlion.tech}"
TESTS_DIR="$(dirname "$0")/tests"
EXPORT_DIR="${EXPORT_DIR:-/tmp/pbr-e2e-export}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
PASSED=0
FAILED=0
SKIPPED=0

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_test() {
    echo -e "${GREEN}[TEST]${NC} $1"
}

run_test() {
    local name="$1"
    local cmd="$2"

    log_test "Running: $name"
    if eval "$cmd"; then
        log_info "PASSED: $name"
        ((PASSED++))
        return 0
    else
        log_error "FAILED: $name"
        ((FAILED++))
        return 1
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    rm -rf "$EXPORT_DIR"
}

trap cleanup EXIT

# ============================================================================
# Test Suite: Basic Module Operations
# ============================================================================
test_basic_module() {
    log_info "=== Test Suite: Basic Module Operations ==="
    cd "$TESTS_DIR/basic"

    # Test 1: Lint the proto files
    run_test "Basic: buf lint" "buf lint"

    # Test 2: Build the proto files
    run_test "Basic: buf build" "buf build"

    # Test 3: Push the module (creates if not exists)
    run_test "Basic: buf push --create" "buf push --create"

    # Test 4: List modules for the owner
    run_test "Basic: buf registry module list" \
        "buf registry module list ${REGISTRY_HOST}/e2e"

    # Test 5: Export the module
    mkdir -p "$EXPORT_DIR/basic"
    run_test "Basic: buf export" "buf export . -o $EXPORT_DIR/basic"

    # Test 6: Verify exported files
    run_test "Basic: verify export" "test -f $EXPORT_DIR/basic/basic.proto"
}

# ============================================================================
# Test Suite: Module with Dependencies
# ============================================================================
test_deps_module() {
    log_info "=== Test Suite: Module with Dependencies ==="
    cd "$TESTS_DIR/deps"

    # Test 1: Update dependencies
    run_test "Deps: buf dep update" "buf dep update"

    # Test 2: Verify buf.lock was created
    run_test "Deps: verify buf.lock exists" "test -f buf.lock"

    # Test 3: Lint with dependencies
    run_test "Deps: buf lint" "buf lint"

    # Test 4: Build with dependencies
    run_test "Deps: buf build" "buf build"

    # Test 5: Push the module
    run_test "Deps: buf push --create" "buf push --create"

    # Test 6: Export with dependencies
    mkdir -p "$EXPORT_DIR/deps"
    run_test "Deps: buf export" "buf export . -o $EXPORT_DIR/deps"

    # Test 7: Verify dependency was included in export
    run_test "Deps: verify dependency export" "test -f $EXPORT_DIR/deps/basic.proto"
}

# ============================================================================
# Test Suite: Labels/Versions
# ============================================================================
test_labels_module() {
    log_info "=== Test Suite: Labels/Versions ==="
    cd "$TESTS_DIR/labels"

    # Test 1: Push with default label (main)
    run_test "Labels: buf push --create (main)" "buf push --create"

    # Test 2: Push with custom label v1.0.0
    run_test "Labels: buf push --label v1.0.0" "buf push --label v1.0.0"

    # Test 3: Export from main label
    mkdir -p "$EXPORT_DIR/labels-main"
    run_test "Labels: export main" \
        "buf export ${REGISTRY_HOST}/e2e/labels:main -o $EXPORT_DIR/labels-main"

    # Test 4: Export from v1.0.0 label
    mkdir -p "$EXPORT_DIR/labels-v1"
    run_test "Labels: export v1.0.0" \
        "buf export ${REGISTRY_HOST}/e2e/labels:v1.0.0 -o $EXPORT_DIR/labels-v1"
}

# ============================================================================
# Test Suite: Module Information
# ============================================================================
test_module_info() {
    log_info "=== Test Suite: Module Information ==="

    # Test 1: List all modules for e2e owner
    run_test "Info: list modules" \
        "buf registry module list ${REGISTRY_HOST}/e2e"

    # Test 2: Get module info for basic
    run_test "Info: get basic module" \
        "buf registry module info ${REGISTRY_HOST}/e2e/basic"

    # Test 3: Get module info for deps
    run_test "Info: get deps module" \
        "buf registry module info ${REGISTRY_HOST}/e2e/deps"

    # Test 4: Get module info for labels
    run_test "Info: get labels module" \
        "buf registry module info ${REGISTRY_HOST}/e2e/labels"
}

# ============================================================================
# Test Suite: Commit Operations
# ============================================================================
test_commits() {
    log_info "=== Test Suite: Commit Operations ==="

    # Test 1: List commits for basic module
    run_test "Commits: list basic commits" \
        "buf registry commit list ${REGISTRY_HOST}/e2e/basic"

    # Test 2: Get latest commit info
    run_test "Commits: get latest basic commit" \
        "buf registry commit info ${REGISTRY_HOST}/e2e/basic:main"
}

# ============================================================================
# Test Suite: Error Cases
# ============================================================================
test_error_cases() {
    log_info "=== Test Suite: Error Cases ==="

    # Test 1: Try to get non-existent module (should fail)
    if buf registry module info ${REGISTRY_HOST}/nonexistent/module 2>/dev/null; then
        log_error "FAILED: Should have failed for non-existent module"
        ((FAILED++))
    else
        log_info "PASSED: Correctly failed for non-existent module"
        ((PASSED++))
    fi

    # Test 2: Try to export non-existent label (should fail)
    if buf export ${REGISTRY_HOST}/e2e/basic:nonexistent -o /tmp/should-fail 2>/dev/null; then
        log_error "FAILED: Should have failed for non-existent label"
        ((FAILED++))
        rm -rf /tmp/should-fail
    else
        log_info "PASSED: Correctly failed for non-existent label"
        ((PASSED++))
    fi
}

# ============================================================================
# Test Suite: Code Generation (Optional)
# ============================================================================
test_codegen() {
    log_info "=== Test Suite: Code Generation ==="
    cd "$TESTS_DIR/basic"

    # Check if buf.gen.yaml exists
    if [ -f buf.gen.yaml ]; then
        run_test "Codegen: buf generate" "buf generate"
    else
        log_warn "Skipping codegen tests - no buf.gen.yaml found"
        ((SKIPPED++))
    fi
}

# ============================================================================
# Main Test Runner
# ============================================================================
main() {
    log_info "Starting PBR E2E Test Suite"
    log_info "Registry: $REGISTRY_HOST"
    log_info "Tests Directory: $TESTS_DIR"
    echo ""

    # Check prerequisites
    if ! command -v buf &> /dev/null; then
        log_error "buf CLI not found. Please install buf first."
        exit 1
    fi

    # Create export directory
    mkdir -p "$EXPORT_DIR"

    # Run test suites
    test_basic_module
    echo ""

    test_deps_module
    echo ""

    test_labels_module
    echo ""

    test_module_info
    echo ""

    test_commits
    echo ""

    test_error_cases
    echo ""

    # Optional tests
    # test_codegen
    # echo ""

    # Summary
    echo ""
    log_info "=========================================="
    log_info "E2E Test Suite Complete"
    log_info "=========================================="
    log_info "Passed: $PASSED"
    if [ $FAILED -gt 0 ]; then
        log_error "Failed: $FAILED"
    else
        log_info "Failed: $FAILED"
    fi
    if [ $SKIPPED -gt 0 ]; then
        log_warn "Skipped: $SKIPPED"
    fi
    log_info "=========================================="

    if [ $FAILED -gt 0 ]; then
        exit 1
    fi
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --host)
            REGISTRY_HOST="$2"
            shift 2
            ;;
        --test)
            # Run specific test suite
            case $2 in
                basic)
                    test_basic_module
                    ;;
                deps)
                    test_deps_module
                    ;;
                labels)
                    test_labels_module
                    ;;
                info)
                    test_module_info
                    ;;
                commits)
                    test_commits
                    ;;
                errors)
                    test_error_cases
                    ;;
                *)
                    log_error "Unknown test suite: $2"
                    exit 1
                    ;;
            esac
            exit 0
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --host HOST    Registry host (default: pbr-example.greatlion.tech)"
            echo "  --test SUITE   Run specific test suite (basic, deps, labels, info, commits, errors)"
            echo "  -h, --help     Show this help message"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

main
