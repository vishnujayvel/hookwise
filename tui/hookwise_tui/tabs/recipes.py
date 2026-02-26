"""Recipes tab — Tree view grouped by category with descriptions."""

from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Static, Tree

from hookwise_tui.data import read_config, read_recipes


class RecipesTab(Widget):
    """Recipes — reusable hook configurations organized by category."""

    DEFAULT_CSS = """
    RecipesTab {
        height: auto;
    }
    RecipesTab .recipes-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    RecipesTab .section-title {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
    }
    RecipesTab Tree {
        height: auto;
        max-height: 30;
        margin: 1 0;
    }
    RecipesTab .includes-section {
        padding: 1 2;
        margin: 1 0;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()
        recipes = read_recipes(config)

        yield Static(
            "Recipes are reusable YAML configs for guards, coaching, and handlers. "
            "Include them in hookwise.yaml with the includes: directive.",
            classes="recipes-intro",
        )

        # Active includes
        includes = config.get("includes", [])
        if isinstance(includes, list) and includes:
            yield Static("Active Includes", classes="section-title")
            with Static(classes="includes-section") as s:
                pass
            for inc in includes:
                yield Static(f"  [green]●[/green] {inc}")

        # Recipe tree
        if recipes:
            yield Static("Available Recipes", classes="section-title")

            tree: Tree[str] = Tree("recipes/")
            tree.root.expand()

            # Group by category
            categories: dict[str, list] = {}
            for r in recipes:
                categories.setdefault(r.category, []).append(r)

            for cat in sorted(categories.keys()):
                cat_node = tree.root.add(f"[bold]{cat}/[/bold]", expand=True)
                for recipe in categories[cat]:
                    status = "[green]●[/green]" if recipe.active else "[dim]○[/dim]"
                    label = f"{status} {recipe.name}"
                    leaf = cat_node.add_leaf(label)
                    if recipe.description:
                        cat_node.add_leaf(
                            f"  [dim italic]{recipe.description[:80]}[/dim italic]"
                        )

            yield tree
        else:
            yield Static(
                "[dim]No recipes found. Check the recipes/ directory.[/dim]"
            )
