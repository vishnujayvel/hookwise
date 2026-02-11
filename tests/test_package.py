"""Tests for hookwise package structure and imports."""

from __future__ import annotations


class TestPackageImports:
    """Verify that the package structure is correct and importable."""

    def test_import_hookwise(self) -> None:
        """Should be able to import the hookwise package."""
        import hookwise

        assert hookwise.__version__ == "0.1.0"

    def test_public_api_exports(self) -> None:
        """All public API symbols should be importable from hookwise."""
        from hookwise import (
            FailOpen,
            atomic_write_json,
            ensure_state_dir,
            get_state_dir,
            safe_read_json,
            setup_logging,
        )

        assert callable(atomic_write_json)
        assert callable(safe_read_json)
        assert callable(ensure_state_dir)
        assert callable(get_state_dir)
        assert callable(setup_logging)
        assert callable(FailOpen)

    def test_subpackages_importable(self) -> None:
        """All subpackages should be importable."""
        import hookwise.analytics
        import hookwise.coaching
        import hookwise.config
        import hookwise.dispatcher
        import hookwise.guards
        import hookwise.status_line
        import hookwise.testing

    def test_version_in_all(self) -> None:
        """__version__ should be in __all__."""
        import hookwise

        assert "__version__" in hookwise.__all__

    def test_cli_importable(self) -> None:
        """CLI module should be importable."""
        from hookwise.cli import main

        assert callable(main)
