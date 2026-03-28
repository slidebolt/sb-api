import os


def get_base_url() -> str:
    base_url = os.getenv("SB_API_BASE_URL", "").strip()
    if base_url:
        return base_url.rstrip("/")

    listen_addr = os.getenv("SB_API_LISTEN_ADDR", "").strip()
    if listen_addr:
        return f"http://{listen_addr}"

    port = os.getenv("SB_API_PORT", "").strip()
    if port:
        return f"http://127.0.0.1:{port}"

    raise RuntimeError(
        "Set SB_API_BASE_URL or SB_API_PORT (or SB_API_LISTEN_ADDR) before running this script."
    )
