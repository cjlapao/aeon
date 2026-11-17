import type { SVGProps } from 'react';
export const AeonIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="100%" height="100%" {...props}>
    <defs>
      <mask id="cleanMonoMask">
        <rect width="512" height="512" fill="#FFFFFF" />
        <circle cx="116" cy="166" r="23" fill="#000000" />
        <circle cx="116" cy="346" r="23" fill="#000000" />
        <circle cx="256" cy="256" r="24" fill="#000000" />
        <circle cx="396" cy="166" r="23" fill="#000000" />
        <circle cx="396" cy="346" r="23" fill="#000000" />
      </mask>
    </defs>

    <g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round">
      <path
        d="M 256,256 C 210,210 170,166 116,166 C 58,166 22,206 22,256 C 22,306 58,346 116,346 C 170,346 210,302 256,256 C 302,210 342,166 396,166 C 454,166 490,206 490,256 C 490,306 454,346 396,346 C 342,346 302,302 256,256 Z"
        stroke-width="40"
        mask="url(#cleanMonoMask)"
      />
      <circle cx="116" cy="166" r="16" stroke-width="8" />
      <circle cx="116" cy="166" r="6" fill="currentColor" stroke="none" />
      <circle cx="256" cy="256" r="18" stroke-width="9" />
      <circle cx="256" cy="256" r="7" fill="currentColor" stroke="none" />
      <circle cx="396" cy="166" r="16" stroke-width="8" />
      <circle cx="396" cy="166" r="6" fill="currentColor" stroke="none" />
      <circle cx="396" cy="346" r="16" stroke-width="8" />
      <circle cx="396" cy="346" r="6" fill="currentColor" stroke="none" />
      <circle cx="116" cy="346" r="16" stroke-width="8" />
      <circle cx="116" cy="346" r="6" fill="currentColor" stroke="none" />
    </g>
  </svg>
);
