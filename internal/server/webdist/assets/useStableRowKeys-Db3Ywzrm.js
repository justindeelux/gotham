<<<<<<<< HEAD:internal/server/webdist/assets/useStableRowKeys-8b6TauZQ.js
import{P as a,p as c}from"./index-BIJkYB3R.js";function r(u){const e=c([]);let l=0;const s=()=>`row-${l+=1}`;a(u,t=>{for(;e.value.length<t;)e.value.push(s());e.value.length>t&&e.value.splice(t)},{immediate:!0,flush:"sync"});function i(t){e.value.splice(t,0,s())}function n(t){e.value.splice(t,1)}return{keys:e,insertAt:i,removeAt:n}}export{r as u};
========
import{P as a,p as c}from"./index-DIyvUrSq.js";function r(u){const e=c([]);let l=0;const s=()=>`row-${l+=1}`;a(u,t=>{for(;e.value.length<t;)e.value.push(s());e.value.length>t&&e.value.splice(t)},{immediate:!0,flush:"sync"});function i(t){e.value.splice(t,0,s())}function n(t){e.value.splice(t,1)}return{keys:e,insertAt:i,removeAt:n}}export{r as u};
>>>>>>>> 02141533 (JUS-66 merge-prep: rebase GS-10 onto main (GS-7 Dockerfile merged)):internal/server/webdist/assets/useStableRowKeys-Db3Ywzrm.js
