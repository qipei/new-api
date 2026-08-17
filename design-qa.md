# Homepage design QA

- Source visual truth: `/Users/qipei/Desktop/桌面文件/2026新项目/token01/Token01 站点重设计/uploads/exec-2a67e92c-e8df-4045-9633-ead474b1e3e8.png`
- Focused sources: `/var/folders/c2/myp2vqn12cgdbpdw6cpy76780000gn/T/TemporaryItems/NSIRD_screencaptureui_bro6NI/截屏2026-08-14 19.08.59.png` and `/var/folders/c2/myp2vqn12cgdbpdw6cpy76780000gn/T/TemporaryItems/NSIRD_screencaptureui_a8idPs/截屏2026-08-14 19.09.39.png`
- Implementation: `http://localhost:3000/?previewDefaultHome=1`
- Implementation screenshots: `/Users/qipei/.codex/visualizations/2026/08/14/019fffcf-12a8-74d2-a268-990cc888e316/homepage-qa-rounding-final.png` and `/Users/qipei/.codex/visualizations/2026/08/14/019fffcf-12a8-74d2-a268-990cc888e316/homepage-qa-compact-final.png`
- Comparison image: `/Users/qipei/.codex/visualizations/2026/08/14/019fffcf-12a8-74d2-a268-990cc888e316/homepage-radius-comparison.png`
- Latest focused implementation: `/Users/qipei/.codex/visualizations/2026/08/14/019fffcf-12a8-74d2-a268-990cc888e316/homepage-login-button-final.png`
- Latest focused comparison: `/Users/qipei/.codex/visualizations/2026/08/14/019fffcf-12a8-74d2-a268-990cc888e316/homepage-login-button-comparison.png`
- State: Chinese, light theme, anonymous user, existing MySQL preview data, rankings module disabled.
- Viewport: in-app browser wide desktop override, 2048 × 1200; implementation capture 2037 × 1686 pixels. The source is 1024 × 1536 pixels. The combined comparison normalizes both sides into 1024 × 1536 panels without judging browser-density differences as design drift.
- Latest focused state: anonymous user, Chinese, light theme, viewport 691 × 996 CSS pixels at DPR 2. The source and implementation header crops were normalized to the same 220-pixel comparison height.

## Full-view comparison evidence

The side-by-side comparison confirms the warm off-white palette, heavy Noto Sans SC typography, yellow accent, rounded cards, model-pricing section, CTA hierarchy, and token01 brand treatment. Live ranking content intentionally differs from the static mock: the MySQL data currently has two models with usage in the last seven days, so the homepage renders those two rather than six fake entries.

## Focused comparison evidence

The focused sources establish two requirements: the compact header must show `豆比特`, and the gateway logo must use the same corner radius as the header logo. The inspected comparison image shows the source states beside the final implementation. Browser-computed styles confirm both logo wrappers render at `10px`, while the compact header DOM reads `豆比特`.

The latest focused comparison places the source registration control beside the implemented login control. Both use the same fixed black surface and white label. Browser-computed styles confirm the implementation is `rgb(20, 20, 20)` with white text and a `10px` radius; pointer hover changes to `rgb(255, 200, 0)` with dark text, matching the design source's registration-button hover specification.

## Findings

- No remaining P0, P1, or P2 mismatch in the requested correction scope.
- Fonts and typography: Noto Sans SC and IBM Plex Mono remain applied; the Chinese copy now reads “一个 API”.
- Spacing and layout rhythm: existing responsive layout and card spacing remain intact.
- Corner radii: design-specific values are explicit rather than theme-relative: gateway card `20px`, application and model cards `16px`, product cards `18px`, product images `20px`, primary/secondary buttons and endpoint field `12px`, copy control `8px`, model pills `10px`, and both Token01 logo treatments `10px`. Fully circular/pill elements remain fully rounded. The pre-existing dynamic navigation container itself remains untouched.
- Colors and tokens: `01` uses the design color `#e2a600`; the yellow underline and CTA treatment remain consistent.
- Header authentication control: the anonymous desktop login button now adopts the design's registration-button palette (`#141414`/white, then `#ffc800`/`#141414` on hover).
- Image and icon fidelity: the supplied Token01 arcade asset is preserved; “更多模型” now uses the OpenRouter library icon.
- Copy and content: the footer Chinese punctuation is removed, model cards are non-links, and price labels come from `/api/pricing` when a matching model exists.

## Comparison history

1. Initial issues: footer punctuation, incorrect more-models icon, dynamic `New API` header label, untranslated “One API”, ranking query gated by navigation visibility, linked ranking cards, missing live prices, and unaccented `01` in the gateway label.
2. Fixes: corrected copy and wordmark component, replaced the icon, added a homepage-only ranking endpoint independent of navigation visibility, merged `/api/pricing` data by model name, and changed ranking cards to non-interactive articles.
3. Follow-up fixes: the compact brand now changes to `豆比特`; the gateway logo matches the header logo at `10px`; theme-relative radius classes on the designed homepage were replaced with the design's exact pixel values.
4. Post-fix evidence: browser DOM shows the corrected labels, OpenRouter icon, two live MySQL-ranked models with API-derived prices, no card links, and the expected computed radii (`10px`, `16px`, `18px`, and `20px` for the inspected elements).
5. Latest correction: replaced the theme-primary login color with the source registration-button palette. The focused combined image and browser-computed default/hover colors show no remaining P0/P1/P2 mismatch in this scope.

## Interactions checked

- Custom homepage remains the normal root response when configured.
- The localhost-only preview query exposes the new default without altering the existing MySQL `HomePageContent` value.
- Dashboard, documentation, pricing, and all-model links retain their configured destinations.
- Model cards have no navigation target.
- Browser console: no warnings or errors.

final result: passed
