/**
 * Recipes tab — lists included recipes/config files grouped by category.
 */

import React from "react";
import { Text, Box } from "ink";
import type { HooksConfig } from "../../../core/types.js";

export interface RecipesTabProps {
  config: HooksConfig;
  onConfigChange: (config: HooksConfig) => void;
}

interface RecipeInfo {
  path: string;
  category: string;
  name: string;
}

function parseRecipePath(path: string): RecipeInfo {
  // Extract category and name from paths like "recipes/safety/block-dangerous.yaml"
  const parts = path.split("/");
  if (parts.length >= 3 && parts[0] === "recipes") {
    return {
      path,
      category: parts[1],
      name: parts.slice(2).join("/").replace(/\.yaml$/, ""),
    };
  }
  return {
    path,
    category: "custom",
    name: parts[parts.length - 1].replace(/\.yaml$/, ""),
  };
}

export function RecipesTab({ config }: RecipesTabProps): React.ReactElement {
  const recipes = config.includes.map(parseRecipePath);

  // Group by category
  const grouped: Record<string, RecipeInfo[]> = {};
  for (const recipe of recipes) {
    if (!grouped[recipe.category]) {
      grouped[recipe.category] = [];
    }
    grouped[recipe.category].push(recipe);
  }

  const categories = Object.keys(grouped).sort();

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Recipes & Includes
      </Text>

      {recipes.length === 0 ? (
        <Box marginTop={1}>
          <Text dimColor>
            No recipes included. Add recipes to the "includes" section of
            hookwise.yaml.
          </Text>
        </Box>
      ) : (
        categories.map((cat) => (
          <Box key={cat} flexDirection="column" marginTop={1}>
            <Text bold color="cyan">
              {cat}
            </Text>
            {grouped[cat].map((recipe, i) => (
              <Box key={i} gap={1} paddingLeft={2}>
                <Text color="green">{"\u2713"}</Text>
                <Text>{recipe.name}</Text>
                <Text dimColor>({recipe.path})</Text>
              </Box>
            ))}
          </Box>
        ))
      )}
    </Box>
  );
}
