import{c3 as E,a as B,V as f,W as a,ak as T,aJ as z,bS as me,cN as xe,cO as ge,d as W,X as O,ar as pe,o as S,c as $,_,ad as R,e as ye,bX as Ce,c9 as ze,ac as we,P as Re,$ as X,s as K,c8 as Se,c7 as J,aB as Te,am as _e,cP as $e,z as I,c$ as Be,aP as t,a1 as G,aS as V,bW as Fe,F as L,n as De,S as Pe}from"./index-B24wX5HB.js";import{u as Y}from"./Input-D2o9mljz.js";var Ae=()=>(()=>{const e=E("75be776d8875fa17");return e[0]||(e[0]=B("svg",{viewBox:"0 0 64 64",class:"check-icon"},[B("path",{d:"M50.42,16.76L22.34,39.45l-8.1-11.46c-1.12-1.58-3.3-1.96-4.88-0.84c-1.58,1.12-1.95,3.3-0.84,4.88l10.26,14.51  c0.56,0.79,1.42,1.31,2.38,1.45c0.16,0.02,0.32,0.03,0.48,0.03c0.8,0,1.57-0.27,2.2-0.78l30.99-25.03c1.5-1.21,1.74-3.42,0.52-4.92  C54.13,15.78,51.93,15.55,50.42,16.76z"})],-1))})(),Me=()=>(()=>{const e=E("c6eed899356c8404");return e[0]||(e[0]=B("svg",{viewBox:"0 0 100 100",class:"line-icon"},[B("path",{d:"M80.2,55.5H21.4c-2.8,0-5.1-2.5-5.1-5.5l0,0c0-3,2.3-5.5,5.1-5.5h58.7c2.8,0,5.1,2.5,5.1,5.5l0,0C85.2,53.1,82.9,55.5,80.2,55.5z"})],-1))})(),Ie=f([a("checkbox",`
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
 `))]);const Ne=["id"],Ue=["tabindex","aria-checked","aria-labelledby","onKeyup","onKeydown","onClick"],Ke={...O.props,size:String,checked:{type:[Boolean,String,Number],default:void 0},defaultChecked:{type:[Boolean,String,Number],default:!1},value:[String,Number],disabled:{type:Boolean,default:void 0},indeterminate:Boolean,label:String,focusable:{type:Boolean,default:!0},checkedValue:{type:[Boolean,String,Number],default:!0},uncheckedValue:{type:[Boolean,String,Number],default:!1},"onUpdate:checked":[Function,Array],onUpdateChecked:[Function,Array],privateInsideTable:Boolean,onChange:[Function,Array]};var Ve=W({name:"Checkbox",props:Ke,setup(e){const c=Re(q,null),u=K(null),{mergedClsPrefixRef:b,inlineThemeDisabled:p,mergedRtlRef:y,mergedComponentPropsRef:F}=X(e),h=K(e.defaultChecked),n=V(e,"checked"),A=Y(n,h),C=Se(()=>{if(c){const o=c.valueSetRef.value;return o&&e.value!==void 0?o.has(e.value):!1}else return A.value===e.checkedValue}),w=J(e,{mergedSize(o){const{size:x}=e;if(x!==void 0)return x;if(c){const{value:s}=c.mergedSizeRef;if(s!==void 0)return s}if(o){const{mergedSize:s}=o;if(s!==void 0)return s.value}const g=F?.value?.Checkbox?.size;return g||"medium"},mergedDisabled(o){const{disabled:x}=e;if(x!==void 0)return x;if(c){if(c.disabledRef.value)return!0;const{maxRef:{value:g},checkedCountRef:s}=c;if(g!==void 0&&s.value>=g&&!C.value)return!0;const{minRef:{value:D}}=c;if(D!==void 0&&s.value<=D&&C.value)return!0}return o?o.disabled.value:!1}}),{mergedDisabledRef:r,mergedSizeRef:k}=w,l=O("Checkbox","-checkbox",Ie,Be,e,b);function i(o){if(c&&e.value!==void 0)c.toggleCheckbox(!C.value,e.value);else{const{onChange:x,"onUpdate:checked":g,onUpdateChecked:s}=e,{nTriggerFormInput:D,nTriggerFormChange:U}=w,P=C.value?e.uncheckedValue:e.checkedValue;g&&t(g,P,o),s&&t(s,P,o),x&&t(x,P,o),D(),U(),h.value=P}}function v(o){r.value||i(o)}function m(o){if(!r.value)switch(o.key){case" ":case"Enter":i(o)}}function d(o){o.key===" "&&o.preventDefault()}const M={focus:()=>{u.value?.focus()},blur:()=>{u.value?.blur()}},N=Te("Checkbox",y,b),H=I(()=>{const{value:o}=k,{common:{cubicBezierEaseInOut:x},self:{borderRadius:g,color:s,colorChecked:D,colorDisabled:U,colorTableHeader:P,colorTableHeaderModal:Q,colorTableHeaderPopover:Z,checkMarkColor:ee,checkMarkColorDisabled:oe,border:re,borderFocus:ae,borderDisabled:ce,borderChecked:ne,boxShadowFocus:le,textColor:te,textColorDisabled:ie,checkMarkColorDisabledChecked:de,colorDisabledChecked:se,borderDisabledChecked:be,labelPadding:ue,labelLineHeight:he,labelFontWeight:fe,[G("fontSize",o)]:ke,[G("size",o)]:ve}}=l.value;return{"--n-label-line-height":he,"--n-label-font-weight":fe,"--n-size":ve,"--n-bezier":x,"--n-border-radius":g,"--n-border":re,"--n-border-checked":ne,"--n-border-focus":ae,"--n-border-disabled":ce,"--n-border-disabled-checked":be,"--n-box-shadow-focus":le,"--n-color":s,"--n-color-checked":D,"--n-color-table":P,"--n-color-table-modal":Q,"--n-color-table-popover":Z,"--n-color-disabled":U,"--n-color-disabled-checked":se,"--n-text-color":te,"--n-text-color-disabled":ie,"--n-check-mark-color":ee,"--n-check-mark-color-disabled":oe,"--n-check-mark-color-disabled-checked":de,"--n-font-size":ke,"--n-label-padding":ue}}),j=p?_e("checkbox",I(()=>k.value[0]),H,e):void 0;return Object.assign(w,M,{rtlEnabled:N,selfRef:u,mergedClsPrefix:b,mergedDisabled:r,renderedChecked:C,mergedTheme:l,labelId:$e(),handleClick:v,handleKeyUp:m,handleKeyDown:d,cssVars:p?void 0:H,themeClass:j?.themeClass,onRender:j?.onRender})},render(){const{$slots:e,renderedChecked:c,mergedDisabled:u,indeterminate:b,privateInsideTable:p,cssVars:y,labelId:F,label:h,mergedClsPrefix:n,focusable:A,handleKeyUp:C,handleKeyDown:w,handleClick:r}=this;this.onRender?.();const k=pe(e.default,l=>h||l?(S(),$("span",{key:1,class:R(`${n}-checkbox__label`),id:F},[_(()=>h||l)],10,Ne)):null);return(()=>{const l=E("70be6e74cd27cb50");return S(),$("div",{ref:"selfRef",class:R([`${n}-checkbox`,this.themeClass,this.rtlEnabled&&`${n}-checkbox--rtl`,c&&`${n}-checkbox--checked`,u&&`${n}-checkbox--disabled`,b&&`${n}-checkbox--indeterminate`,p&&`${n}-checkbox--inside-table`,k&&`${n}-checkbox--show-label`]),tabindex:u||!A?void 0:0,role:"checkbox","aria-checked":b?"mixed":c,"aria-labelledby":F,style:we(y),onKeyup:C,onKeydown:w,onClick:r,onMousedown:l[0]||(l[0]=()=>{ze("selectstart",window,i=>{i.preventDefault()},{once:!0})})},[B("div",{class:R(`${n}-checkbox-box-wrapper`)},[l[1]||(l[1]=_(" ",-1)),B("div",{class:R(`${n}-checkbox-box`)},[ye(Ce,null,{default:()=>this.indeterminate?(S(),$("div",{key:"indeterminate",class:R(`${n}-checkbox-icon`)},[_(()=>Me())],2)):(S(),$("div",{key:"check",class:R(`${n}-checkbox-icon`)},[_(()=>Ae())],2))},1024),B("div",{class:R(`${n}-checkbox-box__border`)},null,2)],2)],2),_(()=>k)],46,Ue)})()}});const q=Fe("n-checkbox-group"),Ee={min:Number,max:Number,size:String,options:Array,labelField:{type:String,default:"label"},valueField:{type:String,default:"value"},value:Array,defaultValue:{type:Array,default:null},disabled:{type:Boolean,default:void 0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onChange:[Function,Array]};var Ge=W({name:"CheckboxGroup",props:Ee,setup(e){const{mergedClsPrefixRef:c}=X(e),u=J(e),{mergedSizeRef:b,mergedDisabledRef:p}=u,y=K(e.defaultValue),F=I(()=>e.value),h=Y(F,y),n=I(()=>h.value?.length||0),A=I(()=>Array.isArray(h.value)?new Set(h.value):new Set);function C(w,r){const{nTriggerFormInput:k,nTriggerFormChange:l}=u,{onChange:i,"onUpdate:value":v,onUpdateValue:m}=e;if(Array.isArray(h.value)){const d=Array.from(h.value),M=d.findIndex(N=>N===r);w?~M||(d.push(r),m&&t(m,d,{actionType:"check",value:r}),v&&t(v,d,{actionType:"check",value:r}),k(),l(),y.value=d,i&&t(i,d)):~M&&(d.splice(M,1),m&&t(m,d,{actionType:"uncheck",value:r}),v&&t(v,d,{actionType:"uncheck",value:r}),i&&t(i,d),y.value=d,k(),l())}else w?(m&&t(m,[r],{actionType:"check",value:r}),v&&t(v,[r],{actionType:"check",value:r}),i&&t(i,[r]),y.value=[r],k(),l()):(m&&t(m,[],{actionType:"uncheck",value:r}),v&&t(v,[],{actionType:"uncheck",value:r}),i&&t(i,[]),y.value=[],k(),l())}return Pe(q,{checkedCountRef:n,maxRef:V(e,"max"),minRef:V(e,"min"),valueSetRef:A,disabledRef:p,mergedSizeRef:b,toggleCheckbox:C}),{mergedClsPrefix:c}},render(){const{options:e,labelField:c,valueField:u}=this.$props;return S(),$("div",{class:R(`${this.mergedClsPrefix}-checkbox-group`),role:"group"},[e?(S(),$(L,{key:0},[_(()=>e.map(b=>{const p=b[u];return S(),De(Ve,{key:p,value:p,disabled:b.disabled,label:b[c]},null,8,["value","disabled","label"])}))],64)):(S(),$(L,{key:1},[_(()=>this.$slots.default?.())],64))],2)}});export{Ve as C,Ge as a};
