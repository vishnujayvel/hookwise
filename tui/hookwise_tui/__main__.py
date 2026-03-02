"""Entry point for `python3 -m hookwise_tui`."""

from hookwise_tui.app import HookwiseTUI


def main() -> None:
    app = HookwiseTUI()
    app.run()


if __name__ == "__main__":
    main()
