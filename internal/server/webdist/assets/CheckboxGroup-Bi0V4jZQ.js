import{bF as E,a as D,S as f,T as a,ah as T,aD as z,bx as me,bc as xe,bd as ge,d as O,U as J,ao as pe,o as S,c as F,X as _,aa as R,e as ye,bz as Ce,bL as ze,a9 as we,M as Re,Y as W,q as N,bK as Se,bJ as Y,av as Te,aj as _e,cl as Fe,y as U,cx as De,aJ as t,_ as G,aM as V,bb as $e,F as L,m as Be,O as Me}from"./index-BHG8dWpL.js";import{u as q}from"./Input-DRfQkNnP.js";var Ae=()=>(()=>{const e=E("75be776d8875fa17");return e[0]||(e[0]=D("svg",{viewBox:"0 0 64 64",class:"check-icon"},[D("path",{d:"M50.42,16.76L22.34,39.45l-8.1-11.46c-1.12-1.58-3.3-1.96-4.88-0.84c-1.58,1.12-1.95,3.3-0.84,4.88l10.26,14.51  c0.56,0.79,1.42,1.31,2.38,1.45c0.16,0.02,0.32,0.03,0.48,0.03c0.8,0,1.57-0.27,2.2-0.78l30.99-25.03c1.5-1.21,1.74-3.42,0.52-4.92  C54.13,15.78,51.93,15.55,50.42,16.76z"})],-1))})(),Ie=()=>(()=>{const e=E("c6eed899356c8404");return e[0]||(e[0]=D("svg",{viewBox:"0 0 100 100",class:"line-icon"},[D("path",{d:"M80.2,55.5H21.4c-2.8,0-5.1-2.5-5.1-5.5l0,0c0-3,2.3-5.5,5.1-5.5h58.7c2.8,0,5.1,2.5,5.1,5.5l0,0C85.2,53.1,82.9,55.5,80.2,55.5z"})],-1))})(),Ue=f([a("checkbox",`
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
 `))]);const Ke=["id"],Pe=["tabindex","aria-checked","aria-labelledby","onKeyup","onKeydown","onClick"],Ne={...J.props,size:String,checked:{type:[Boolean,String,Number],default:void 0},defaultChecked:{type:[Boolean,String,Number],default:!1},value:[String,Number],disabled:{type:Boolean,default:void 0},indeterminate:Boolean,label:String,focusable:{type:Boolean,default:!0},checkedValue:{type:[Boolean,String,Number],default:!0},uncheckedValue:{type:[Boolean,String,Number],default:!1},"onUpdate:checked":[Function,Array],onUpdateChecked:[Function,Array],privateInsideTable:Boolean,onChange:[Function,Array]};var Ve=O({name:"Checkbox",props:Ne,setup(e){const n=Re(X,null),u=N(null),{mergedClsPrefixRef:b,inlineThemeDisabled:p,mergedRtlRef:y,mergedComponentPropsRef:$}=W(e),h=N(e.defaultChecked),c=V(e,"checked"),A=q(c,h),C=Se(()=>{if(n){const o=n.valueSetRef.value;return o&&e.value!==void 0?o.has(e.value):!1}else return A.value===e.checkedValue}),w=Y(e,{mergedSize(o){const{size:x}=e;if(x!==void 0)return x;if(n){const{value:s}=n.mergedSizeRef;if(s!==void 0)return s}if(o){const{mergedSize:s}=o;if(s!==void 0)return s.value}const g=$?.value?.Checkbox?.size;return g||"medium"},mergedDisabled(o){const{disabled:x}=e;if(x!==void 0)return x;if(n){if(n.disabledRef.value)return!0;const{maxRef:{value:g},checkedCountRef:s}=n;if(g!==void 0&&s.value>=g&&!C.value)return!0;const{minRef:{value:B}}=n;if(B!==void 0&&s.value<=B&&C.value)return!0}return o?o.disabled.value:!1}}),{mergedDisabledRef:r,mergedSizeRef:k}=w,l=J("Checkbox","-checkbox",Ue,De,e,b);function i(o){if(n&&e.value!==void 0)n.toggleCheckbox(!C.value,e.value);else{const{onChange:x,"onUpdate:checked":g,onUpdateChecked:s}=e,{nTriggerFormInput:B,nTriggerFormChange:P}=w,M=C.value?e.uncheckedValue:e.checkedValue;g&&t(g,M,o),s&&t(s,M,o),x&&t(x,M,o),B(),P(),h.value=M}}function v(o){r.value||i(o)}function m(o){if(!r.value)switch(o.key){case" ":case"Enter":i(o)}}function d(o){o.key===" "&&o.preventDefault()}const I={focus:()=>{u.value?.focus()},blur:()=>{u.value?.blur()}},K=Te("Checkbox",y,b),j=U(()=>{const{value:o}=k,{common:{cubicBezierEaseInOut:x},self:{borderRadius:g,color:s,colorChecked:B,colorDisabled:P,colorTableHeader:M,colorTableHeaderModal:Q,colorTableHeaderPopover:Z,checkMarkColor:ee,checkMarkColorDisabled:oe,border:re,borderFocus:ae,borderDisabled:ne,borderChecked:ce,boxShadowFocus:le,textColor:te,textColorDisabled:ie,checkMarkColorDisabledChecked:de,colorDisabledChecked:se,borderDisabledChecked:be,labelPadding:ue,labelLineHeight:he,labelFontWeight:fe,[G("fontSize",o)]:ke,[G("size",o)]:ve}}=l.value;return{"--n-label-line-height":he,"--n-label-font-weight":fe,"--n-size":ve,"--n-bezier":x,"--n-border-radius":g,"--n-border":re,"--n-border-checked":ce,"--n-border-focus":ae,"--n-border-disabled":ne,"--n-border-disabled-checked":be,"--n-box-shadow-focus":le,"--n-color":s,"--n-color-checked":B,"--n-color-table":M,"--n-color-table-modal":Q,"--n-color-table-popover":Z,"--n-color-disabled":P,"--n-color-disabled-checked":se,"--n-text-color":te,"--n-text-color-disabled":ie,"--n-check-mark-color":ee,"--n-check-mark-color-disabled":oe,"--n-check-mark-color-disabled-checked":de,"--n-font-size":ke,"--n-label-padding":ue}}),H=p?_e("checkbox",U(()=>k.value[0]),j,e):void 0;return Object.assign(w,I,{rtlEnabled:K,selfRef:u,mergedClsPrefix:b,mergedDisabled:r,renderedChecked:C,mergedTheme:l,labelId:Fe(),handleClick:v,handleKeyUp:m,handleKeyDown:d,cssVars:p?void 0:j,themeClass:H?.themeClass,onRender:H?.onRender})},render(){const{$slots:e,renderedChecked:n,mergedDisabled:u,indeterminate:b,privateInsideTable:p,cssVars:y,labelId:$,label:h,mergedClsPrefix:c,focusable:A,handleKeyUp:C,handleKeyDown:w,handleClick:r}=this;this.onRender?.();const k=pe(e.default,l=>h||l?(S(),F("span",{key:1,class:R(`${c}-checkbox__label`),id:$},[_(()=>h||l)],10,Ke)):null);return(()=>{const l=E("70be6e74cd27cb50");return S(),F("div",{ref:"selfRef",class:R([`${c}-checkbox`,this.themeClass,this.rtlEnabled&&`${c}-checkbox--rtl`,n&&`${c}-checkbox--checked`,u&&`${c}-checkbox--disabled`,b&&`${c}-checkbox--indeterminate`,p&&`${c}-checkbox--inside-table`,k&&`${c}-checkbox--show-label`]),tabindex:u||!A?void 0:0,role:"checkbox","aria-checked":b?"mixed":n,"aria-labelledby":$,style:we(y),onKeyup:C,onKeydown:w,onClick:r,onMousedown:l[0]||(l[0]=()=>{ze("selectstart",window,i=>{i.preventDefault()},{once:!0})})},[D("div",{class:R(`${c}-checkbox-box-wrapper`)},[l[1]||(l[1]=_(" ",-1)),D("div",{class:R(`${c}-checkbox-box`)},[ye(Ce,null,{default:()=>this.indeterminate?(S(),F("div",{key:"indeterminate",class:R(`${c}-checkbox-icon`)},[_(()=>Ie())],2)):(S(),F("div",{key:"check",class:R(`${c}-checkbox-icon`)},[_(()=>Ae())],2))},1024),D("div",{class:R(`${c}-checkbox-box__border`)},null,2)],2)],2),_(()=>k)],46,Pe)})()}});const X=$e("n-checkbox-group"),Ee={min:Number,max:Number,size:String,options:Array,labelField:{type:String,default:"label"},valueField:{type:String,default:"value"},value:Array,defaultValue:{type:Array,default:null},disabled:{type:Boolean,default:void 0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onChange:[Function,Array]};var Ge=O({name:"CheckboxGroup",props:Ee,setup(e){const{mergedClsPrefixRef:n}=W(e),u=Y(e),{mergedSizeRef:b,mergedDisabledRef:p}=u,y=N(e.defaultValue),$=U(()=>e.value),h=q($,y),c=U(()=>h.value?.length||0),A=U(()=>Array.isArray(h.value)?new Set(h.value):new Set);function C(w,r){const{nTriggerFormInput:k,nTriggerFormChange:l}=u,{onChange:i,"onUpdate:value":v,onUpdateValue:m}=e;if(Array.isArray(h.value)){const d=Array.from(h.value),I=d.findIndex(K=>K===r);w?~I||(d.push(r),m&&t(m,d,{actionType:"check",value:r}),v&&t(v,d,{actionType:"check",value:r}),k(),l(),y.value=d,i&&t(i,d)):~I&&(d.splice(I,1),m&&t(m,d,{actionType:"uncheck",value:r}),v&&t(v,d,{actionType:"uncheck",value:r}),i&&t(i,d),y.value=d,k(),l())}else w?(m&&t(m,[r],{actionType:"check",value:r}),v&&t(v,[r],{actionType:"check",value:r}),i&&t(i,[r]),y.value=[r],k(),l()):(m&&t(m,[],{actionType:"uncheck",value:r}),v&&t(v,[],{actionType:"uncheck",value:r}),i&&t(i,[]),y.value=[],k(),l())}return Me(X,{checkedCountRef:c,maxRef:V(e,"max"),minRef:V(e,"min"),valueSetRef:A,disabledRef:p,mergedSizeRef:b,toggleCheckbox:C}),{mergedClsPrefix:n}},render(){const{options:e,labelField:n,valueField:u}=this.$props;return S(),F("div",{class:R(`${this.mergedClsPrefix}-checkbox-group`),role:"group"},[e?(S(),F(L,{key:0},[_(()=>e.map(b=>{const p=b[u];return S(),Be(Ve,{key:p,value:p,disabled:b.disabled,label:b[n]},null,8,["value","disabled","label"])}))],64)):(S(),F(L,{key:1},[_(()=>this.$slots.default?.())],64))],2)}});export{Ve as C,Ge as a};
