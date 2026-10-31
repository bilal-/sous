// Execute the upstream scope function without generating or seeding summaries.
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const { buildProfileIsolationScope: profileScope } = await import(pathToFileURL(
  resolve(process.argv[2], "MemoryCore/src/core/profile/profile-scope.ts")
));
const first = { teamId: "work", agentId: "shared", userId: "sam", taskId: "api" };
const taskOnly = { ...first, taskId: "site" };
const separateProject = { ...first, agentId: "site-project" };
const separateWorkspace = { ...first, teamId: "personal" };
console.log(JSON.stringify({
  method: "execution of upstream buildProfileIsolationScope; no generated summaries tested",
  task_only_mapping_collides: profileScope(first) === profileScope(taskOnly),
  project_agent_mapping_collides: profileScope(first) === profileScope(separateProject),
  workspace_team_mapping_collides: profileScope(first) === profileScope(separateWorkspace),
}));
