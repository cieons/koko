import { useEffect, useState } from "react";

import { type Space, SpaceList } from "./api/space";
import { call } from "./client";

/**
 * App is the web UI: for now one page that lists the Spaces, until the shell
 * and its pages replace it.
 */
export function App() {
  // undefined until the list arrives: a failed call shows no list, never an
  // empty one.
  const [spaces, setSpaces] = useState<Space[]>();

  useEffect(() => {
    let current = true;
    void call(SpaceList, {}).then((response) => {
      if (current) {
        setSpaces(response.spaces);
      }
    });
    return () => {
      current = false;
    };
  }, []);

  return (
    <main>
      <h1>koko</h1>
      {spaces && (
        <ul>
          {spaces.map((space) => (
            <li key={space.id}>{space.name}</li>
          ))}
        </ul>
      )}
    </main>
  );
}
