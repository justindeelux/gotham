<<<<<<<< HEAD:internal/server/webdist/assets/useStableRowKeys-n69dB05F.js
import{P as a,p as c}from"./index-CmV1hNxG.js";function r(u){const e=c([]);let l=0;const s=()=>`row-${l+=1}`;a(u,t=>{for(;e.value.length<t;)e.value.push(s());e.value.length>t&&e.value.splice(t)},{immediate:!0,flush:"sync"});function i(t){e.value.splice(t,0,s())}function n(t){e.value.splice(t,1)}return{keys:e,insertAt:i,removeAt:n}}export{r as u};
========
import{P as a,p as c}from"./index-CaTGnH17.js";function r(u){const e=c([]);let l=0;const s=()=>`row-${l+=1}`;a(u,t=>{for(;e.value.length<t;)e.value.push(s());e.value.length>t&&e.value.splice(t)},{immediate:!0,flush:"sync"});function i(t){e.value.splice(t,0,s())}function n(t){e.value.splice(t,1)}return{keys:e,insertAt:i,removeAt:n}}export{r as u};
>>>>>>>> 979ef1a2 (JUS-63 GS-7 fix round 2: reviewer findings):internal/server/webdist/assets/useStableRowKeys-buGe6TYA.js
