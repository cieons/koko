import { routes } from "./routes";
import { Shell } from "./shell/Shell";

/** App is the web UI: the shell and its pages. */
export function App() {
  return <Shell routes={routes} />;
}
