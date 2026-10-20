import{ez as E,a as $,b1 as f,b2 as a,bS as T,bT as z,e1 as me,eJ as xe,eK as ge,d as O,b3 as W,e6 as pe,o as S,c as F,b5 as _,bC as R,e as Ce,e7 as ye,eC as ze,V as we,aL as Re,b6 as Y,j as N,eE as Se,fs as J,bK as Te,bY as _e,eL as Fe,f as K,b_ as t,ft as $e,b8 as L,c1 as V,e5 as Be,F as G,p as De,aZ as Ae}from"./index-oDF5Gl01.js";import{u as Z}from"./use-merged-state-BjaCeOTF.js";var Me=()=>(()=>{const e=E("75be776d8875fa17");return e[0]||(e[0]=$("svg",{viewBox:"0 0 64 64",class:"check-icon"},[$("path",{d:"M50.42,16.76L22.34,39.45l-8.1-11.46c-1.12-1.58-3.3-1.96-4.88-0.84c-1.58,1.12-1.95,3.3-0.84,4.88l10.26,14.51  c0.56,0.79,1.42,1.31,2.38,1.45c0.16,0.02,0.32,0.03,0.48,0.03c0.8,0,1.57-0.27,2.2-0.78l30.99-25.03c1.5-1.21,1.74-3.42,0.52-4.92  C54.13,15.78,51.93,15.55,50.42,16.76z"})],-1))})(),Ie=()=>(()=>{const e=E("c6eed899356c8404");return e[0]||(e[0]=$("svg",{viewBox:"0 0 100 100",class:"line-icon"},[$("path",{d:"M80.2,55.5H21.4c-2.8,0-5.1-2.5-5.1-5.5l0,0c0-3,2.3-5.5,5.1-5.5h58.7c2.8,0,5.1,2.5,5.1,5.5l0,0C85.2,53.1,82.9,55.5,80.2,55.5z"})],-1))})(),Ke=f([a("checkbox",`
 font-size: var(--n-font-size);
 outline: none;
 cursor: pointer;
 display: inline-flex;
 flex-wrap: nowrap;
 align-items: flex-start;
 word-break: break-word;
 line-height: var(--n-size);
 --n-merged-color-table: var(--n-color-table);
 `,[T("show-label","line-height: var(--n-label-line-height);"),f("&:hover",[a("checkbox-box",[z("border","border: var(--n-border-checked);")])]),f("&:focus:not(:active)",[a("checkbox-box",[z("border",`
 border: var(--n-border-focus);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),T("inside-table",[a("checkbox-box",`
 background-color: var(--n-merged-color-table);
 `)]),T("checked",[a("checkbox-box",`
 background-color: var(--n-color-checked);
 `,[a("checkbox-icon",[f(".check-icon",`
 opacity: 1;
 transform: scale(1);
 `)])])]),T("indeterminate",[a("checkbox-box",[a("checkbox-icon",[f(".check-icon",`
 opacity: 0;
 transform: scale(.5);
 `),f(".line-icon",`
 opacity: 1;
 transform: scale(1);
 `)])])]),T("checked, indeterminate",[f("&:focus:not(:active)",[a("checkbox-box",[z("border",`
 border: var(--n-border-checked);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),a("checkbox-box",`
 background-color: var(--n-color-checked);
 border-left: 0;
 border-top: 0;
 `,[z("border",{border:"var(--n-border-checked)"})])]),T("disabled",{cursor:"not-allowed"},[T("checked",[a("checkbox-box",`
 background-color: var(--n-color-disabled-checked);
 `,[z("border",{border:"var(--n-border-disabled-checked)"}),a("checkbox-icon",[f(".check-icon, .line-icon",{fill:"var(--n-check-mark-color-disabled-checked)"})])])]),a("checkbox-box",`
 background-color: var(--n-color-disabled);
 `,[z("border",`
 border: var(--n-border-disabled);
 `),a("checkbox-icon",[f(".check-icon, .line-icon",`
 fill: var(--n-check-mark-color-disabled);
 `)])]),z("label",`
 color: var(--n-text-color-disabled);
 `)]),a("checkbox-box-wrapper",`
 position: relative;
 width: var(--n-size);
 flex-shrink: 0;
 flex-grow: 0;
 user-select: none;
 -webkit-user-select: none;
 `),a("checkbox-box",`
 position: absolute;
 left: 0;
 top: 50%;
 transform: translateY(-50%);
 height: var(--n-size);
 width: var(--n-size);
 display: inline-block;
 box-sizing: border-box;
 border-radius: var(--n-border-radius);
 background-color: var(--n-color);
 transition: background-color 0.3s var(--n-bezier);
 `,[z("border",`
 transition:
 border-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border: var(--n-border);
 `),a("checkbox-icon",`
 display: flex;
 align-items: center;
 justify-content: center;
 position: absolute;
 left: 1px;
 right: 1px;
 top: 1px;
 bottom: 1px;
 `,[f(".check-icon, .line-icon",`
 width: 100%;
 fill: var(--n-check-mark-color);
 opacity: 0;
 transform: scale(0.5);
 transform-origin: center;
 transition:
 fill 0.3s var(--n-bezier),
 transform 0.3s var(--n-bezier),
 opacity 0.3s var(--n-bezier),
 border-color 0.3s var(--n-bezier);
 `),me({left:"1px",top:"1px"})])]),z("label",`
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 user-select: none;
 -webkit-user-select: none;
 padding: var(--n-label-padding);
 font-weight: var(--n-label-font-weight);
 `,[f("&:empty",{display:"none"})])]),xe(a("checkbox",`
 --n-merged-color-table: var(--n-color-table-modal);
 `)),ge(a("checkbox",`
 --n-merged-color-table: var(--n-color-table-popover);
 `))]);const Pe=["id"],Ue=["tabindex","aria-checked","aria-labelledby","onKeyup","onKeydown","onClick"],Ne={...W.props,size:String,checked:{type:[Boolean,String,Number],default:void 0},defaultChecked:{type:[Boolean,String,Number],default:!1},value:[String,Number],disabled:{type:Boolean,default:void 0},indeterminate:Boolean,label:String,focusable:{type:Boolean,default:!0},checkedValue:{type:[Boolean,String,Number],default:!0},uncheckedValue:{type:[Boolean,String,Number],default:!1},"onUpdate:checked":[Function,Array],onUpdateChecked:[Function,Array],privateInsideTable:Boolean,onChange:[Function,Array]};var Ve=O({name:"Checkbox",props:Ne,setup(e){const n=Re(q,null),u=N(null),{mergedClsPrefixRef:b,inlineThemeDisabled:p,mergedRtlRef:C,mergedComponentPropsRef:B}=Y(e),h=N(e.defaultChecked),c=V(e,"checked"),M=Z(c,h),y=Se(()=>{if(n){const o=n.valueSetRef.value;return o&&e.value!==void 0?o.has(e.value):!1}else return M.value===e.checkedValue}),w=J(e,{mergedSize(o){const{size:x}=e;if(x!==void 0)return x;if(n){const{value:s}=n.mergedSizeRef;if(s!==void 0)return s}if(o){const{mergedSize:s}=o;if(s!==void 0)return s.value}const g=B?.value?.Checkbox?.size;return g||"medium"},mergedDisabled(o){const{disabled:x}=e;if(x!==void 0)return x;if(n){if(n.disabledRef.value)return!0;const{maxRef:{value:g},checkedCountRef:s}=n;if(g!==void 0&&s.value>=g&&!y.value)return!0;const{minRef:{value:D}}=n;if(D!==void 0&&s.value<=D&&y.value)return!0}return o?o.disabled.value:!1}}),{mergedDisabledRef:r,mergedSizeRef:k}=w,l=W("Checkbox","-checkbox",Ke,$e,e,b);function i(o){if(n&&e.value!==void 0)n.toggleCheckbox(!y.value,e.value);else{const{onChange:x,"onUpdate:checked":g,onUpdateChecked:s}=e,{nTriggerFormInput:D,nTriggerFormChange:U}=w,A=y.value?e.uncheckedValue:e.checkedValue;g&&t(g,A,o),s&&t(s,A,o),x&&t(x,A,o),D(),U(),h.value=A}}function v(o){r.value||i(o)}function m(o){if(!r.value)switch(o.key){case" ":case"Enter":i(o)}}function d(o){o.key===" "&&o.preventDefault()}const I={focus:()=>{u.value?.focus()},blur:()=>{u.value?.blur()}},P=Te("Checkbox",C,b),j=K(()=>{const{value:o}=k,{common:{cubicBezierEaseInOut:x},self:{borderRadius:g,color:s,colorChecked:D,colorDisabled:U,colorTableHeader:A,colorTableHeaderModal:Q,colorTableHeaderPopover:X,checkMarkColor:ee,checkMarkColorDisabled:oe,border:re,borderFocus:ae,borderDisabled:ne,borderChecked:ce,boxShadowFocus:le,textColor:te,textColorDisabled:ie,checkMarkColorDisabledChecked:de,colorDisabledChecked:se,borderDisabledChecked:be,labelPadding:ue,labelLineHeight:he,labelFontWeight:fe,[L("fontSize",o)]:ke,[L("size",o)]:ve}}=l.value;return{"--n-label-line-height":he,"--n-label-font-weight":fe,"--n-size":ve,"--n-bezier":x,"--n-border-radius":g,"--n-border":re,"--n-border-checked":ce,"--n-border-focus":ae,"--n-border-disabled":ne,"--n-border-disabled-checked":be,"--n-box-shadow-focus":le,"--n-color":s,"--n-color-checked":D,"--n-color-table":A,"--n-color-table-modal":Q,"--n-color-table-popover":X,"--n-color-disabled":U,"--n-color-disabled-checked":se,"--n-text-color":te,"--n-text-color-disabled":ie,"--n-check-mark-color":ee,"--n-check-mark-color-disabled":oe,"--n-check-mark-color-disabled-checked":de,"--n-font-size":ke,"--n-label-padding":ue}}),H=p?_e("checkbox",K(()=>k.value[0]),j,e):void 0;return Object.assign(w,I,{rtlEnabled:P,selfRef:u,mergedClsPrefix:b,mergedDisabled:r,renderedChecked:y,mergedTheme:l,labelId:Fe(),handleClick:v,handleKeyUp:m,handleKeyDown:d,cssVars:p?void 0:j,themeClass:H?.themeClass,onRender:H?.onRender})},render(){const{$slots:e,renderedChecked:n,mergedDisabled:u,indeterminate:b,privateInsideTable:p,cssVars:C,labelId:B,label:h,mergedClsPrefix:c,focusable:M,handleKeyUp:y,handleKeyDown:w,handleClick:r}=this;this.onRender?.();const k=pe(e.default,l=>h||l?(S(),F("span",{key:1,class:R(`${c}-checkbox__label`),id:B},[_(()=>h||l)],10,Pe)):null);return(()=>{const l=E("70be6e74cd27cb50");return S(),F("div",{ref:"selfRef",class:R([`${c}-checkbox`,this.themeClass,this.rtlEnabled&&`${c}-checkbox--rtl`,n&&`${c}-checkbox--checked`,u&&`${c}-checkbox--disabled`,b&&`${c}-checkbox--indeterminate`,p&&`${c}-checkbox--inside-table`,k&&`${c}-checkbox--show-label`]),tabindex:u||!M?void 0:0,role:"checkbox","aria-checked":b?"mixed":n,"aria-labelledby":B,style:we(C),onKeyup:y,onKeydown:w,onClick:r,onMousedown:l[0]||(l[0]=()=>{ze("selectstart",window,i=>{i.preventDefault()},{once:!0})})},[$("div",{class:R(`${c}-checkbox-box-wrapper`)},[l[1]||(l[1]=_(" ",-1)),$("div",{class:R(`${c}-checkbox-box`)},[Ce(ye,null,{default:()=>this.indeterminate?(S(),F("div",{key:"indeterminate",class:R(`${c}-checkbox-icon`)},[_(()=>Ie())],2)):(S(),F("div",{key:"check",class:R(`${c}-checkbox-icon`)},[_(()=>Me())],2))},1024),$("div",{class:R(`${c}-checkbox-box__border`)},null,2)],2)],2),_(()=>k)],46,Ue)})()}});const q=Be("n-checkbox-group"),Ee={min:Number,max:Number,size:String,options:Array,labelField:{type:String,default:"label"},valueField:{type:String,default:"value"},value:Array,defaultValue:{type:Array,default:null},disabled:{type:Boolean,default:void 0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onChange:[Function,Array]};var Le=O({name:"CheckboxGroup",props:Ee,setup(e){const{mergedClsPrefixRef:n}=Y(e),u=J(e),{mergedSizeRef:b,mergedDisabledRef:p}=u,C=N(e.defaultValue),B=K(()=>e.value),h=Z(B,C),c=K(()=>h.value?.length||0),M=K(()=>Array.isArray(h.value)?new Set(h.value):new Set);function y(w,r){const{nTriggerFormInput:k,nTriggerFormChange:l}=u,{onChange:i,"onUpdate:value":v,onUpdateValue:m}=e;if(Array.isArray(h.value)){const d=Array.from(h.value),I=d.findIndex(P=>P===r);w?~I||(d.push(r),m&&t(m,d,{actionType:"check",value:r}),v&&t(v,d,{actionType:"check",value:r}),k(),l(),C.value=d,i&&t(i,d)):~I&&(d.splice(I,1),m&&t(m,d,{actionType:"uncheck",value:r}),v&&t(v,d,{actionType:"uncheck",value:r}),i&&t(i,d),C.value=d,k(),l())}else w?(m&&t(m,[r],{actionType:"check",value:r}),v&&t(v,[r],{actionType:"check",value:r}),i&&t(i,[r]),C.value=[r],k(),l()):(m&&t(m,[],{actionType:"uncheck",value:r}),v&&t(v,[],{actionType:"uncheck",value:r}),i&&t(i,[]),C.value=[],k(),l())}return Ae(q,{checkedCountRef:c,maxRef:V(e,"max"),minRef:V(e,"min"),valueSetRef:M,disabledRef:p,mergedSizeRef:b,toggleCheckbox:y}),{mergedClsPrefix:n}},render(){const{options:e,labelField:n,valueField:u}=this.$props;return S(),F("div",{class:R(`${this.mergedClsPrefix}-checkbox-group`),role:"group"},[e?(S(),F(G,{key:0},[_(()=>e.map(b=>{const p=b[u];return S(),De(Ve,{key:p,value:p,disabled:b.disabled,label:b[n]},null,8,["value","disabled","label"])}))],64)):(S(),F(G,{key:1},[_(()=>this.$slots.default?.())],64))],2)}});export{Ve as C,Le as a};
