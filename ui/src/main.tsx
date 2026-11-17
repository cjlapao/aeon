import React from "react";
import { createRoot } from "react-dom/client";
import { ProductApp } from "./app/router";

import "@fontsource-variable/inter";
import "./index.css";
// The kit's prebuilt utilities must follow Aeon's Tailwind output: its dark
// variants pair with light utilities on the same element (e.g. floating menu).
import "@cjlapao/ui-kit/styles.css";

const el = document.getElementById("root");
if (!el) throw new Error("#root is missing from index.html");

createRoot(el).render(
  <React.StrictMode>
    <ProductApp />
  </React.StrictMode>,
);
